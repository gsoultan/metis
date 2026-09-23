package impl

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
)

const (
	workerID = "messaging-bridge"
	maxTasks = 10
	// lockDurationMS is how long a fetched task stays invisible to other
	// workers. The repository treats this value as milliseconds; the previous
	// constant was named ...Sec and passed 30, so bridge locks expired after
	// thirty milliseconds and every poll re-fetched and re-published the same
	// tasks.
	lockDurationMS    = 30_000
	pollInterval      = 5 * time.Second
	reconnectInterval = 5 * time.Second

	inboundDispatchMaxAttempts    = 3
	inboundDispatchInitialBackoff = 200 * time.Millisecond
	inboundDispatchMaxBackoff     = 2 * time.Second
	inboundDispatchMaxJitter      = 200 * time.Millisecond

	// inboundPrefetch is how many unacknowledged messages the broker will hand
	// this consumer at once. One: the consumer processes serially, and a
	// prefetch larger than that only decides how many messages are stranded on
	// a consumer that dies.
	inboundPrefetch = 1

	inboundDeadLetterQueueSuffix    = ".dlq"
	inboundDeadLetterPublishTimeout = 3 * time.Second
	inboundDispatchTimeout          = 10 * time.Second

	inboundPartitionWorkerCount = 16
	inboundPartitionQueueSize   = 64
)

var (
	errInboundDeadLetterPublishTimeout = errors.New("inbound dead-letter publish timeout")
	errInboundDispatchTimeout          = errors.New("inbound message dispatch timeout")
)

type messagingService struct {
	engine                   contracts.EngineEventBus
	externalSvc              contracts.ExternalTaskService
	sleep                    func(ctx context.Context, delay time.Duration) error
	jitter                   func(max time.Duration) time.Duration
	inboundDispatchTimeout   time.Duration
	inboundPartitionExecutor *inboundPartitionExecutor
	cancels                  sync.Map // string -> context.CancelFunc
	wg                       sync.WaitGroup
}

func NewMessagingService(engine contracts.EngineEventBus, externalSvc contracts.ExternalTaskService) contracts.MessagingService {
	return &messagingService{
		engine:                   engine,
		externalSvc:              externalSvc,
		sleep:                    sleepWithContext,
		jitter:                   randomJitter,
		inboundDispatchTimeout:   inboundDispatchTimeout,
		inboundPartitionExecutor: newInboundPartitionExecutor(inboundPartitionWorkerCount, inboundPartitionQueueSize),
	}
}

func (s *messagingService) StartBridge(ctx context.Context, projectID uuid.UUID, topic string, rabbitURL string, exchange string, routingKey string) error {
	id := fmt.Sprintf("bridge-%s-%s", projectID, topic)
	if _, loaded := s.cancels.Load(id); loaded {
		return fmt.Errorf("bridge for topic %s already running", topic)
	}

	childCtx, cancel := context.WithCancel(ctx)
	s.cancels.Store(id, cancel)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.cancels.Delete(id)
		s.runBridge(childCtx, projectID, topic, rabbitURL, exchange, routingKey)
	}()

	return nil
}

func (s *messagingService) runBridge(ctx context.Context, projectID uuid.UUID, topic string, rabbitURL string, exchange string, routingKey string) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var conn *amqp.Connection
	var ch *amqp.Channel
	var err error

	cleanup := func() {
		if ch != nil {
			closeQuietly(ch, "AMQP channel")
		}
		if conn != nil {
			closeQuietly(conn, "AMQP connection")
		}
		ch = nil
		conn = nil
	}
	defer cleanup()

	var publisher *confirmingPublisher

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if conn == nil || conn.IsClosed() {
				cleanup()
				conn, err = amqp.Dial(rabbitURL)
				if err != nil {
					log.Error().Err(err).Msg("Bridge RabbitMQ connection error")
					continue
				}
				ch, err = conn.Channel()
				if err != nil {
					log.Error().Err(err).Msg("Bridge RabbitMQ channel error")
					cleanup()
					continue
				}
				// Once per channel: NotifyReturn appends a listener each call,
				// and this loop publishes for as long as the connection lives.
				publisher, err = newConfirmingPublisher(ch)
				if err != nil {
					log.Error().Err(err).Msg("Bridge could not enable publisher confirms")
					cleanup()
					continue
				}
			}

			// Fetch and lock tasks
			tasks, err := s.externalSvc.FetchAndLock(ctx, topic, workerID, maxTasks, lockDurationMS)
			if err != nil {
				log.Error().Err(err).Msg("Bridge fetch error")
				continue
			}

			for _, task := range tasks {
				body, err := json.Marshal(task)
				if err != nil {
					// Publishing "null" onto a work queue hands a worker a
					// message it cannot act on and loses the task.
					log.Error().Err(err).Str("taskId", task.ID.String()).
						Msg("A task could not be encoded and was not published")
					s.releaseUnforwardedTask(ctx, task, err)
					continue
				}
				err = publisher.publish(ctx, exchange, routingKey, amqp.Publishing{
					ContentType: "application/json",
					Body:        body,
					Headers: amqp.Table{
						"task_id": task.ID.String(),
					},
				})
				if err != nil {
					// The task is locked to this bridge. Leaving it that way
					// meant the work stalled until the lock expired while the
					// line below claimed it had been forwarded — so hand it
					// back now, and let the retry the engine already has do its
					// job.
					log.Error().Err(err).Str("taskID", task.ID.String()).
						Str("exchange", exchange).Str("routingKey", routingKey).
						Msg("A task was not accepted by the broker and was handed back")
					s.releaseUnforwardedTask(ctx, task, err)
					continue
				}
				log.Info().Str("taskID", task.ID.String()).Msg("Forwarded external task to RabbitMQ")
			}
		}
	}
}

func (s *messagingService) StartInboundConsumer(ctx context.Context, projectID uuid.UUID, rabbitURL string, queueName string, messageName string) error {
	id := fmt.Sprintf("consumer-%s-%s", projectID, queueName)
	if _, loaded := s.cancels.Load(id); loaded {
		return fmt.Errorf("consumer for queue %s already running", queueName)
	}

	childCtx, cancel := context.WithCancel(ctx)
	s.cancels.Store(id, cancel)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.cancels.Delete(id)
		s.runConsumer(childCtx, projectID, rabbitURL, queueName, messageName)
	}()

	return nil
}

func (s *messagingService) runConsumer(ctx context.Context, projectID uuid.UUID, rabbitURL string, queueName string, messageName string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			err := s.consumeOnce(ctx, projectID, rabbitURL, queueName, messageName)
			if err != nil {
				log.Error().Err(err).Dur("retryIn", reconnectInterval).Msg("Consumer error; retrying")
				if sleepErr := sleepWithContext(ctx, reconnectInterval); sleepErr != nil {
					return
				}
			}
		}
	}
}

func (s *messagingService) consumeOnce(ctx context.Context, projectID uuid.UUID, rabbitURL string, queueName string, messageName string) error {
	conn, err := amqp.Dial(rabbitURL)
	if err != nil {
		return err
	}
	defer closeQuietly(conn, "AMQP connection")

	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer closeQuietly(ch, "AMQP channel")

	q, err := ch.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		return err
	}

	dlqName := q.Name + inboundDeadLetterQueueSuffix
	if _, err = ch.QueueDeclare(dlqName, true, false, false, false, nil); err != nil {
		return err
	}

	// The dead-letter publish has to be accounted for before the message it is
	// standing in for can be acknowledged, so the channel needs confirms.
	publisher, err := newConfirmingPublisher(ch)
	if err != nil {
		return err
	}

	// Manual acknowledgement, so one message at a time is outstanding and the
	// broker holds the rest.
	if err := ch.Qos(inboundPrefetch, 0, false); err != nil {
		return err
	}

	// autoAck was true, which acknowledges a message the moment the broker
	// hands it over — before anything has looked at it. Every path below that
	// tries to preserve a message it could not process was therefore preserving
	// one the broker had already forgotten: if the dead-letter publish failed,
	// or the engine was shutting down, the message was simply gone.
	msgs, err := ch.ConsumeWithContext(ctx, q.Name, "", false, false, false, false, nil)
	if err != nil {
		return err
	}

	publishToQueue := func(ctx context.Context, queueName string, message amqp.Publishing) error {
		// The default exchange routes by queue name, and the queue is declared
		// above, so this is routable — but it still has to be confirmed, or a
		// broker that refused it would look identical to one that took it.
		return publisher.publish(ctx, "", queueName, message)
	}

	for d := range msgs {
		outcome, err := s.processInboundDelivery(ctx, projectID, q.Name, dlqName, messageName, d, publishToQueue)
		if err != nil {
			log.Error().Err(err).Str("queue", q.Name).Str("deadLetterQueue", dlqName).Msg("Error processing inbound message")
		}
		settleInboundDelivery(d, outcome, q.Name)
	}

	return nil
}

// deliveryOutcome is what should happen to a message once it has been handled.
type deliveryOutcome int

const (
	// ackDelivery means somebody has taken responsibility for the message: it
	// was dispatched, or it is parked in the dead-letter queue.
	ackDelivery deliveryOutcome = iota
	// requeueDelivery means nobody has, so the broker should keep it. It covers
	// the two cases that used to lose a message outright — a dead-letter
	// publish that failed, and a shutdown arriving mid-dispatch.
	requeueDelivery
)

// settleInboundDelivery tells the broker what happened to a message.
//
// A failure to settle is logged rather than returned: the message stays
// unacknowledged, so the broker redelivers it when this consumer's channel
// closes, which is the outcome this function was trying to produce anyway.
func settleInboundDelivery(delivery amqp.Delivery, outcome deliveryOutcome, queueName string) {
	var err error
	switch outcome {
	case requeueDelivery:
		err = delivery.Nack(false, true)
	default:
		err = delivery.Ack(false)
	}
	if err != nil {
		log.Error().Err(err).Str("queue", queueName).
			Msg("Could not tell the broker what happened to a message; it will be redelivered")
	}
}

func (s *messagingService) processInboundDelivery(
	ctx context.Context,
	projectID uuid.UUID,
	queueName string,
	dlqName string,
	messageName string,
	delivery amqp.Delivery,
	publishToQueue func(context.Context, string, amqp.Publishing) error,
) (deliveryOutcome, error) {
	payload, correlationKey, err := decodeInboundPayload(delivery)
	if err != nil {
		dlqErr := s.publishInboundDeadLetter(ctx, publishToQueue, dlqName, queueName, messageName, correlationKey, nil, delivery.Body, "unmarshal_error", err)
		if dlqErr != nil {
			// The body is unreadable and the dead-letter queue would not take
			// it. Requeueing keeps it somewhere rather than nowhere.
			return requeueDelivery, errors.Join(err, dlqErr)
		}

		return ackDelivery, fmt.Errorf("unmarshal inbound message: %w", err)
	}

	log.Info().Str("messageName", messageName).Str("correlationKey", correlationKey).Msg("Received inbound message")
	err = s.dispatchInboundMessage(ctx, projectID, messageName, correlationKey, payload)
	if err == nil {
		return ackDelivery, nil
	}

	if !isRetryableDispatchError(err) {
		// Cancelled or timed out, which here means the engine is stopping. The
		// message was not refused by anything — nobody got to it — so it goes
		// back rather than being dropped on the way out.
		return requeueDelivery, err
	}

	dlqErr := s.publishInboundDeadLetter(ctx, publishToQueue, dlqName, queueName, messageName, correlationKey, payload, delivery.Body, "dispatch_failed", err)
	if dlqErr != nil {
		return requeueDelivery, errors.Join(err, dlqErr)
	}

	return ackDelivery, err
}

func decodeInboundPayload(delivery amqp.Delivery) (map[string]any, string, error) {
	var payload map[string]any
	if err := json.Unmarshal(delivery.Body, &payload); err != nil {
		return nil, correlationKeyFromPayloadOrHeaders(nil, delivery.Headers), err
	}

	return payload, correlationKeyFromPayloadOrHeaders(payload, delivery.Headers), nil
}

func correlationKeyFromPayloadOrHeaders(payload map[string]any, headers amqp.Table) string {
	if payload != nil {
		if correlationKey, ok := payload["correlation_key"].(string); ok && correlationKey != "" {
			return correlationKey
		}
	}

	if correlationKey, ok := headers["correlation_key"].(string); ok {
		return correlationKey
	}

	return ""
}

func (s *messagingService) publishInboundDeadLetter(
	ctx context.Context,
	publishToQueue func(context.Context, string, amqp.Publishing) error,
	dlqName string,
	queueName string,
	messageName string,
	correlationKey string,
	payload map[string]any,
	rawBody []byte,
	failureReason string,
	failureErr error,
) error {
	dlqPayload, err := json.Marshal(map[string]any{
		"original_queue":  queueName,
		"message_name":    messageName,
		"correlation_key": correlationKey,
		"failure_reason":  failureReason,
		"failure_error":   failureErr.Error(),
		"failed_at":       time.Now().UTC(),
		"payload":         payload,
		"raw_body_base64": base64.StdEncoding.EncodeToString(rawBody),
	})
	if err != nil {
		return fmt.Errorf("marshal inbound dead-letter payload: %w", err)
	}

	publishCtx, cancel := context.WithTimeoutCause(ctx, inboundDeadLetterPublishTimeout, errInboundDeadLetterPublishTimeout)
	defer cancel()

	err = publishToQueue(publishCtx, dlqName, amqp.Publishing{
		ContentType: "application/json",
		Body:        dlqPayload,
	})
	if err != nil {
		return fmt.Errorf("publish inbound dead-letter message: %w", err)
	}

	log.Warn().
		Str("deadLetterQueue", dlqName).
		Str("messageName", messageName).
		Str("correlationKey", correlationKey).
		Str("failureReason", failureReason).
		Msg("Moved inbound message to dead-letter queue")

	return nil
}

func (s *messagingService) dispatchInboundMessage(ctx context.Context, projectID uuid.UUID, messageName string, correlationKey string, payload map[string]any) error {
	dispatchCtx, cancel := s.newInboundDispatchContext(ctx)
	defer cancel()

	if s.inboundPartitionExecutor == nil {
		return s.sendMessageWithRetry(dispatchCtx, projectID, messageName, correlationKey, payload)
	}

	return s.inboundPartitionExecutor.Execute(dispatchCtx, correlationKey, func(runCtx context.Context) error {
		return s.sendMessageWithRetry(runCtx, projectID, messageName, correlationKey, payload)
	})
}

func (s *messagingService) newInboundDispatchContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}

	// A message arrives from a broker rather than a request, so it carries no
	// authenticated principal and no tenant. Correlating it has to look across
	// tenants to find the subscription it belongs to; the repository scope
	// narrows to the project once the subscription is known.
	ctx = entities.WithSystemContext(ctx)

	timeout := cmp.Or(s.inboundDispatchTimeout, inboundDispatchTimeout)

	return context.WithTimeoutCause(ctx, timeout, errInboundDispatchTimeout)
}

func (s *messagingService) sendMessageWithRetry(ctx context.Context, projectID uuid.UUID, messageName string, correlationKey string, payload map[string]any) error {
	for attempt := range inboundDispatchMaxAttempts {
		err := s.engine.SendMessage(ctx, projectID, messageName, correlationKey, payload)
		if err == nil {
			return nil
		}

		if !isRetryableDispatchError(err) {
			return fmt.Errorf("send inbound message: %w", err)
		}

		if attempt == inboundDispatchMaxAttempts-1 {
			return fmt.Errorf("send inbound message after %d attempts: %w", inboundDispatchMaxAttempts, err)
		}

		delay := s.retryDelay(attempt)
		log.Warn().
			Err(err).
			Int("attempt", attempt+1).
			Int("maxAttempts", inboundDispatchMaxAttempts).
			Dur("retryIn", delay).
			Msg("Transient inbound message dispatch error; retrying")

		if err := s.sleep(ctx, delay); err != nil {
			return fmt.Errorf("wait before retrying inbound message dispatch: %w", err)
		}
	}

	return nil
}

func (s *messagingService) retryDelay(attempt int) time.Duration {
	baseDelay := inboundDispatchInitialBackoff << attempt
	if baseDelay > inboundDispatchMaxBackoff {
		baseDelay = inboundDispatchMaxBackoff
	}

	return baseDelay + s.jitter(inboundDispatchMaxJitter)
}

func randomJitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}

	// #nosec G404 -- jitter, as in backoff.go. Not used for anything secret.
	return time.Duration(rand.Int64N(int64(max) + 1))
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isRetryableDispatchError(err error) bool {
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func (s *messagingService) StopAll() {
	s.cancels.Range(func(key, value any) bool {
		// Shutdown is the worst moment to panic: the consumers after this one
		// in the range would never be stopped, and the process would hang
		// waiting for them.
		cancel, isCancel := value.(context.CancelFunc)
		if !isCancel {
			log.Error().Interface("key", key).Msg("A consumer's stored cancel was not a cancel function")
			return true
		}
		cancel()
		return true
	})

	if s.inboundPartitionExecutor != nil {
		s.inboundPartitionExecutor.Stop()
	}

	s.wg.Wait()
}

// closeQuietly closes an AMQP handle and says so when it will not close.
//
// A channel or connection that fails to close is one the broker still holds —
// they are finite per connection, and a service that leaks them stops being able
// to open new ones with no clue as to why.
func closeQuietly(handle io.Closer, what string) {
	if err := handle.Close(); err != nil {
		log.Warn().Err(err).Msgf("Could not close the %s", what)
	}
}

// releaseUnforwardedTask hands a locked task back when the bridge could not put
// it on the broker.
//
// FetchAndLock makes a task invisible to other workers for lockDurationMS. A
// bridge that fetched a task and then failed to publish it used to simply move
// on, so the work sat idle for the whole lock — thirty seconds in which the
// only record was a log line saying it had been forwarded. Failing it here
// makes the engine's own retry the thing that decides what happens next, which
// is what it is for.
func (s *messagingService) releaseUnforwardedTask(ctx context.Context, task *entities.ExternalTask, cause error) {
	if s.externalSvc == nil || task == nil {
		return
	}
	// Retries are left where the task already had them: this is a transport
	// failure, not the worker rejecting the work, so it should not consume an
	// attempt the business logic is entitled to.
	err := s.externalSvc.HandleFailure(ctx, task.ID, workerID,
		"the bridge could not publish this task to the broker",
		cause.Error(), task.Retries, 0)
	if err != nil {
		log.Error().Err(err).Str("taskID", task.ID.String()).
			Msg("A task that was not forwarded could not be handed back either; it stays locked until its lock expires")
	}
}
