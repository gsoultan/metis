package impl

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/gsoultan/gobpm/server/domains/entities"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/services/contracts"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog/log"
)

const (
	workerID          = "messaging-bridge"
	maxTasks          = 10
	lockDurationSec   = 30
	pollInterval      = 5 * time.Second
	reconnectInterval = 5 * time.Second

	inboundDispatchMaxAttempts    = 3
	inboundDispatchInitialBackoff = 200 * time.Millisecond
	inboundDispatchMaxBackoff     = 2 * time.Second
	inboundDispatchMaxJitter      = 200 * time.Millisecond

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
			ch.Close()
		}
		if conn != nil {
			conn.Close()
		}
		ch = nil
		conn = nil
	}
	defer cleanup()

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
			}

			// Fetch and lock tasks
			tasks, err := s.externalSvc.FetchAndLock(ctx, topic, workerID, maxTasks, lockDurationSec)
			if err != nil {
				log.Error().Err(err).Msg("Bridge fetch error")
				continue
			}

			for _, task := range tasks {
				body, _ := json.Marshal(task)
				err = ch.PublishWithContext(ctx, exchange, routingKey, false, false, amqp.Publishing{
					ContentType: "application/json",
					Body:        body,
					Headers: amqp.Table{
						"task_id": task.ID.String(),
					},
				})
				if err != nil {
					log.Error().Err(err).Msg("Bridge publish error")
					// Task will timeout and be retried
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
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	q, err := ch.QueueDeclare(queueName, true, false, false, false, nil)
	if err != nil {
		return err
	}

	dlqName := q.Name + inboundDeadLetterQueueSuffix
	if _, err = ch.QueueDeclare(dlqName, true, false, false, false, nil); err != nil {
		return err
	}

	msgs, err := ch.ConsumeWithContext(ctx, q.Name, "", true, false, false, false, nil)
	if err != nil {
		return err
	}

	publishToQueue := func(ctx context.Context, queueName string, message amqp.Publishing) error {
		return ch.PublishWithContext(ctx, "", queueName, false, false, message)
	}

	for d := range msgs {
		err = s.processInboundDelivery(ctx, projectID, q.Name, dlqName, messageName, d, publishToQueue)
		if err != nil {
			log.Error().Err(err).Str("queue", q.Name).Str("deadLetterQueue", dlqName).Msg("Error processing inbound message")
		}
	}

	return nil
}

func (s *messagingService) processInboundDelivery(
	ctx context.Context,
	projectID uuid.UUID,
	queueName string,
	dlqName string,
	messageName string,
	delivery amqp.Delivery,
	publishToQueue func(context.Context, string, amqp.Publishing) error,
) error {
	payload, correlationKey, err := decodeInboundPayload(delivery)
	if err != nil {
		dlqErr := s.publishInboundDeadLetter(ctx, publishToQueue, dlqName, queueName, messageName, correlationKey, nil, delivery.Body, "unmarshal_error", err)
		if dlqErr != nil {
			return errors.Join(err, dlqErr)
		}

		return fmt.Errorf("unmarshal inbound message: %w", err)
	}

	log.Info().Str("messageName", messageName).Str("correlationKey", correlationKey).Msg("Received inbound message")
	err = s.dispatchInboundMessage(ctx, projectID, messageName, correlationKey, payload)
	if err == nil {
		return nil
	}

	if !isRetryableDispatchError(err) {
		return err
	}

	dlqErr := s.publishInboundDeadLetter(ctx, publishToQueue, dlqName, queueName, messageName, correlationKey, payload, delivery.Body, "dispatch_failed", err)
	if dlqErr != nil {
		return errors.Join(err, dlqErr)
	}

	return err
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
		cancel := value.(context.CancelFunc)
		cancel()
		return true
	})

	if s.inboundPartitionExecutor != nil {
		s.inboundPartitionExecutor.Stop()
	}

	s.wg.Wait()
}
