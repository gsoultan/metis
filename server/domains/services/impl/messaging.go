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
	"github.com/rs/zerolog"
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
	lockDurationMS     = 30_000
	bridgePollInterval = 5 * time.Second

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

// brokerReconnects is how long a bridge or consumer waits between attempts to
// reach a broker it cannot: 5 seconds, doubling to 5 minutes, each wait varied
// by a quarter either way, and back to 5 seconds once it has reached it.
//
// It was every 5 seconds for as long as the broker stayed down, so a broker
// gone for a weekend was dialled some fifty thousand times by each bridge and
// consumer of each replica, in step with one another, and logged every time.
var brokerReconnects = backoff{first: 5 * time.Second, most: 5 * time.Minute}

type messagingService struct {
	engine                   contracts.EngineEventBus
	externalSvc              contracts.ExternalTaskService
	sleep                    func(ctx context.Context, delay time.Duration) error
	jitter                   func(max time.Duration) time.Duration
	inboundDispatchTimeout   time.Duration
	inboundPartitionExecutor *inboundPartitionExecutor
	cancels                  sync.Map // string -> context.CancelFunc
	wg                       sync.WaitGroup

	// dial is how a bridge reaches its broker: the AMQP library, or a broker
	// in memory in a test.
	dial func(url string) (brokerConnection, error)
	// confirmTimeout is how long a publish waits for the broker's confirm.
	confirmTimeout time.Duration
	// pollInterval is how long a bridge waits between rounds.
	pollInterval time.Duration
}

func NewMessagingService(engine contracts.EngineEventBus, externalSvc contracts.ExternalTaskService) contracts.MessagingService {
	return &messagingService{
		engine:                   engine,
		externalSvc:              externalSvc,
		sleep:                    sleepWithContext,
		jitter:                   randomJitter,
		inboundDispatchTimeout:   inboundDispatchTimeout,
		inboundPartitionExecutor: newInboundPartitionExecutor(inboundPartitionWorkerCount, inboundPartitionQueueSize),
		dial:                     dialAMQP,
		confirmTimeout:           rabbitMQConfirmTimeout(),
		pollInterval:             bridgePollInterval,
	}
}

func (s *messagingService) StartBridge(ctx context.Context, projectID uuid.UUID, topic string, rabbitURL string, exchange string, routingKey string) error {
	id := fmt.Sprintf("bridge-%s-%s", projectID, topic)
	if _, loaded := s.cancels.Load(id); loaded {
		return fmt.Errorf("bridge for topic %s already running", topic)
	}

	childCtx, cancel := context.WithCancel(ctx)
	s.cancels.Store(id, cancel)

	bridge := s.newBridge(ctx, projectID, topic, rabbitURL, exchange, routingKey)
	s.wg.Go(func() {
		defer s.cancels.Delete(id)
		bridge.run(childCtx)
	})

	return nil
}

// newBridge assembles a bridge from what the service was given.
func (s *messagingService) newBridge(ctx context.Context, projectID uuid.UUID, topic, rabbitURL, exchange, routingKey string) *externalTaskBridge {
	// Every line names the bridge. With several running, one that does not is
	// a line nobody can act on.
	logger := loggerFrom(ctx).With().
		Str("project", projectID.String()).
		Str("topic", topic).
		Str("exchange", exchange).
		Str("routingKey", routingKey).
		Logger()
	return &externalTaskBridge{
		tasks:          s.externalSvc,
		link:           &brokerLink{url: rabbitURL, dial: s.dial},
		topic:          topic,
		exchange:       exchange,
		routingKey:     routingKey,
		pollInterval:   cmp.Or(s.pollInterval, bridgePollInterval),
		confirmTimeout: s.confirmTimeout,
		reconnect:      brokerReconnects,
		sleep:          s.sleep,
		logger:         &logger,
		problems:       problemLog{logger: &logger},
	}
}

func (s *messagingService) StartInboundConsumer(ctx context.Context, projectID uuid.UUID, rabbitURL string, queueName string, messageName string) error {
	id := fmt.Sprintf("consumer-%s-%s", projectID, queueName)
	if _, loaded := s.cancels.Load(id); loaded {
		return fmt.Errorf("consumer for queue %s already running", queueName)
	}

	childCtx, cancel := context.WithCancel(ctx)
	s.cancels.Store(id, cancel)

	consumer := s.newConsumer(ctx, projectID, rabbitURL, queueName, messageName)
	s.wg.Go(func() {
		defer s.cancels.Delete(id)
		consumer.run(childCtx)
	})

	return nil
}

// newConsumer assembles a consumer from what the service was given.
func (s *messagingService) newConsumer(ctx context.Context, projectID uuid.UUID, rabbitURL, queueName, messageName string) *inboundConsumer {
	// Every line names the consumer, as the bridge's do.
	logger := loggerFrom(ctx).With().
		Str("project", projectID.String()).
		Str("queue", queueName).
		Str("messageName", messageName).
		Logger()
	return &inboundConsumer{
		messaging:      s,
		link:           &brokerLink{url: rabbitURL, dial: s.dial},
		project:        projectID,
		queue:          queueName,
		message:        messageName,
		confirmTimeout: s.confirmTimeout,
		reconnect:      brokerReconnects,
		sleep:          s.sleep,
		logger:         &logger,
		problems:       problemLog{logger: &logger},
	}
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

// loggerFrom returns the logger ctx carries, or the global one when it carries
// none.
//
// Whoever starts a bridge or a consumer knows things about it this service
// does not — which connection it reads, which organization it acts for — and
// hands them down on the context's logger, so they are on every line the loop
// writes too.
func loggerFrom(ctx context.Context) *zerolog.Logger {
	if logger := zerolog.Ctx(ctx); logger.GetLevel() != zerolog.Disabled {
		return logger
	}
	return &log.Logger
}

// closeQuietly closes an AMQP handle and says so when it will not close.
//
// A channel or connection that fails to close is one the broker still holds —
// they are finite per connection, and a service that leaks them stops being able
// to open new ones with no clue as to why. One the broker has already closed is
// not that, and says nothing.
func closeQuietly(handle io.Closer, what string) {
	if err := handle.Close(); err != nil && !errors.Is(err, amqp.ErrClosed) {
		log.Warn().Err(err).Msgf("Could not close the %s", what)
	}
}
