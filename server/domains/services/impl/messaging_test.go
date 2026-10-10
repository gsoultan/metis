package impl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	amqp "github.com/rabbitmq/amqp091-go"
)

type engineEventBusStub struct {
	sendMessage      func(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string, vars map[string]any) error
	startFromMessage func(ctx context.Context, projectID uuid.UUID, messageName string, vars map[string]any) (int, error)
}

// StartFromMessage reports one start unless told otherwise, so a test that is
// not about keyless messages is not dead-lettered for starting nothing.
func (s *engineEventBusStub) StartFromMessage(ctx context.Context, projectID uuid.UUID, messageName string, vars map[string]any) (int, error) {
	if s.startFromMessage == nil {
		return 1, nil
	}

	return s.startFromMessage(ctx, projectID, messageName, vars)
}

func (s *engineEventBusStub) DispatchEvent(_ context.Context, _ entities.ProcessEvent) {}

func (s *engineEventBusStub) BroadcastSignal(_ context.Context, _ uuid.UUID, _ string, _ map[string]any) error {
	return nil
}

func (s *engineEventBusStub) SendMessage(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string, vars map[string]any) error {
	if s.sendMessage == nil {
		return nil
	}

	return s.sendMessage(ctx, projectID, messageName, correlationKey, vars)
}

func (s *engineEventBusStub) TriggerEscalation(_ context.Context, _ *entities.ProcessInstance, _ *entities.ProcessDefinition, _ entities.Node, _ string) error {
	return nil
}

func (s *engineEventBusStub) TriggerCompensation(_ context.Context, _ *entities.ProcessInstance, _ *entities.ProcessDefinition, _ entities.Node, _ string) error {
	return nil
}

func TestMessagingServiceSendMessageWithRetry(t *testing.T) {
	t.Parallel()

	testProjectID := uuid.New()
	testPayload := map[string]any{"correlation_key": "corr-1"}
	transientErr := errors.New("temporary dispatch error")

	tests := []struct {
		name             string
		sendMessage      func(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string, vars map[string]any) error
		sleep            func(ctx context.Context, delay time.Duration) error
		expectedAttempts int
		expectedSleeps   []time.Duration
		expectedErrIs    error
		expectedErrText  string
	}{
		{
			name: "success on first attempt",
			sendMessage: func(context.Context, uuid.UUID, string, string, map[string]any) error {
				return nil
			},
			expectedAttempts: 1,
		},
		{
			name: "success after transient retries",
			sendMessage: func() func(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string, vars map[string]any) error {
				attempt := 0
				return func(context.Context, uuid.UUID, string, string, map[string]any) error {
					attempt++
					if attempt < 3 {
						return transientErr
					}
					return nil
				}
			}(),
			expectedAttempts: 3,
			expectedSleeps: []time.Duration{
				inboundDispatchInitialBackoff,
				inboundDispatchInitialBackoff * 2,
			},
		},
		{
			name: "non-retryable context cancellation error",
			sendMessage: func(context.Context, uuid.UUID, string, string, map[string]any) error {
				return context.Canceled
			},
			expectedAttempts: 1,
			expectedErrIs:    context.Canceled,
			expectedErrText:  "send inbound message",
		},
		{
			name: "terminal failure after max attempts",
			sendMessage: func(context.Context, uuid.UUID, string, string, map[string]any) error {
				return transientErr
			},
			expectedAttempts: inboundDispatchMaxAttempts,
			expectedSleeps: []time.Duration{
				inboundDispatchInitialBackoff,
				inboundDispatchInitialBackoff * 2,
			},
			expectedErrIs:   transientErr,
			expectedErrText: fmt.Sprintf("after %d attempts", inboundDispatchMaxAttempts),
		},
		{
			name: "context canceled while waiting for retry",
			sendMessage: func(context.Context, uuid.UUID, string, string, map[string]any) error {
				return transientErr
			},
			sleep: func(context.Context, time.Duration) error {
				return context.Canceled
			},
			expectedAttempts: 1,
			expectedSleeps: []time.Duration{
				inboundDispatchInitialBackoff,
			},
			expectedErrIs:   context.Canceled,
			expectedErrText: "wait before retrying inbound message dispatch",
		},
	}

	for _, tc := range tests {

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			attempts := 0
			sleepDelays := make([]time.Duration, 0)

			engine := &engineEventBusStub{
				sendMessage: func(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string, vars map[string]any) error {
					attempts++
					return tc.sendMessage(ctx, projectID, messageName, correlationKey, vars)
				},
			}

			sleepFn := tc.sleep
			if sleepFn == nil {
				sleepFn = func(context.Context, time.Duration) error {
					return nil
				}
			}

			svc := &messagingService{
				engine: engine,
				sleep: func(ctx context.Context, delay time.Duration) error {
					sleepDelays = append(sleepDelays, delay)
					return sleepFn(ctx, delay)
				},
				jitter: func(max time.Duration) time.Duration {
					return 0
				},
			}

			err := svc.sendMessageWithRetry(ctx, testProjectID, "msg", "corr-1", testPayload)

			if tc.expectedErrIs == nil && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}
			if tc.expectedErrIs != nil {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !errors.Is(err, tc.expectedErrIs) {
					t.Fatalf("expected error to match %v, got %v", tc.expectedErrIs, err)
				}
			}

			if tc.expectedErrText != "" && (err == nil || !strings.Contains(err.Error(), tc.expectedErrText)) {
				t.Fatalf("expected error text to include %q, got %v", tc.expectedErrText, err)
			}

			if attempts != tc.expectedAttempts {
				t.Fatalf("expected %d attempts, got %d", tc.expectedAttempts, attempts)
			}

			if !slices.Equal(sleepDelays, tc.expectedSleeps) {
				t.Fatalf("expected sleep delays %v, got %v", tc.expectedSleeps, sleepDelays)
			}
		})
	}
}

func TestMessagingServiceRetryDelayCapsAtMaxBackoff(t *testing.T) {
	t.Parallel()

	svc := &messagingService{
		jitter: func(max time.Duration) time.Duration {
			return max
		},
	}

	delay := svc.retryDelay(8)
	expected := inboundDispatchMaxBackoff + inboundDispatchMaxJitter
	if delay != expected {
		t.Fatalf("expected delay %v, got %v", expected, delay)
	}
}

func TestMessagingServiceDispatchInboundMessageHonorsDispatchTimeout(t *testing.T) {
	t.Parallel()

	testProjectID := uuid.New()

	tests := []struct {
		name              string
		withPartitionExec bool
	}{
		{
			name:              "without partition executor",
			withPartitionExec: false,
		},
		{
			name:              "with partition executor",
			withPartitionExec: true,
		},
	}

	for _, tc := range tests {

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// atomic: the stub is invoked on the partition-executor goroutine
			// while the test body reads the counter after dispatchInboundMessage
			// returns on timeout, so a plain int is a data race.
			var sendAttempts atomic.Int64
			svc := &messagingService{
				engine: &engineEventBusStub{
					sendMessage: func(ctx context.Context, _ uuid.UUID, _ string, _ string, _ map[string]any) error {
						sendAttempts.Add(1)
						<-ctx.Done()
						return ctx.Err()
					},
				},
				sleep: func(context.Context, time.Duration) error {
					return nil
				},
				jitter: func(time.Duration) time.Duration {
					return 0
				},
				inboundDispatchTimeout: 20 * time.Millisecond,
			}

			if tc.withPartitionExec {
				svc.inboundPartitionExecutor = newInboundPartitionExecutor(2, 2)
				t.Cleanup(func() {
					svc.inboundPartitionExecutor.Stop()
				})
			}

			err := svc.dispatchInboundMessage(t.Context(), testProjectID, "message.name", "corr-timeout", map[string]any{"x": "y"})
			if err == nil {
				t.Fatalf("expected timeout error, got nil")
			}

			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected deadline exceeded, got %v", err)
			}

			if got := sendAttempts.Load(); got != 1 {
				t.Fatalf("expected 1 send attempt, got %d", got)
			}
		})
	}
}

func TestMessagingServiceProcessInboundDeliveryDispatchTimeoutIsNotMovedToDLQ(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	publishCalls := 0
	svc := &messagingService{
		engine: &engineEventBusStub{
			sendMessage: func(ctx context.Context, _ uuid.UUID, _ string, _ string, _ map[string]any) error {
				<-ctx.Done()
				return ctx.Err()
			},
		},
		sleep: func(context.Context, time.Duration) error {
			return nil
		},
		jitter: func(time.Duration) time.Duration {
			return 0
		},
		inboundDispatchTimeout: 20 * time.Millisecond,
	}

	outcome, err := svc.processInboundDelivery(
		ctx,
		uuid.New(),
		"incoming-queue",
		"incoming-queue.dlq",
		"message.name",
		amqp.Delivery{Body: []byte(`{"correlation_key":"corr-timeout","value":"x"}`)},
		func(_ context.Context, _ string, _ amqp.Publishing) error {
			publishCalls++
			return nil
		},
	)

	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}

	if publishCalls != 0 {
		t.Fatalf("expected no DLQ publishes, got %d", publishCalls)
	}

	// A dispatch that ran out of time because the engine is stopping is nobody
	// refusing the message — it is nobody getting to it. It used to be
	// auto-acknowledged on the way out, which lost it.
	if outcome != requeueDelivery {
		t.Fatalf("a message abandoned at shutdown was not requeued (outcome %v)", outcome)
	}
}

func TestMessagingServiceProcessInboundDelivery(t *testing.T) {
	t.Parallel()

	testProjectID := uuid.New()
	transientErr := errors.New("temporary dispatch error")
	dlqPublishErr := errors.New("dlq publish failed")

	tests := []struct {
		name                  string
		delivery              amqp.Delivery
		sendMessage           func(attempt int) error
		publishErr            error
		expectedAttempts      int
		expectedPublishCalls  int
		expectedErrIs         []error
		expectedErrText       string
		expectedFailureReason string
		expectedCorrelation   string
	}{
		{
			name: "success dispatch does not publish to dlq",
			delivery: amqp.Delivery{
				Body: []byte(`{"correlation_key":"corr-1","value":"x"}`),
			},
			sendMessage: func(int) error {
				return nil
			},
			expectedAttempts:     1,
			expectedPublishCalls: 0,
		},
		{
			name: "terminal dispatch failure is published to dlq",
			delivery: amqp.Delivery{
				Body: []byte(`{"correlation_key":"corr-2","value":"x"}`),
			},
			sendMessage: func(int) error {
				return transientErr
			},
			expectedAttempts:      inboundDispatchMaxAttempts,
			expectedPublishCalls:  1,
			expectedErrIs:         []error{transientErr},
			expectedErrText:       fmt.Sprintf("after %d attempts", inboundDispatchMaxAttempts),
			expectedFailureReason: "dispatch_failed",
			expectedCorrelation:   "corr-2",
		},
		{
			name: "unmarshal failure is published to dlq",
			delivery: amqp.Delivery{
				Body:    []byte(`{"correlation_key":"corr-bad"`),
				Headers: amqp.Table{"correlation_key": "corr-header"},
			},
			sendMessage: func(int) error {
				return nil
			},
			expectedAttempts:      0,
			expectedPublishCalls:  1,
			expectedErrText:       "unmarshal inbound message",
			expectedFailureReason: "unmarshal_error",
			expectedCorrelation:   "corr-header",
		},
		{
			name: "dlq publish failure is joined with dispatch error",
			delivery: amqp.Delivery{
				Body: []byte(`{"correlation_key":"corr-3","value":"x"}`),
			},
			sendMessage: func(int) error {
				return transientErr
			},
			publishErr:            dlqPublishErr,
			expectedAttempts:      inboundDispatchMaxAttempts,
			expectedPublishCalls:  1,
			expectedErrIs:         []error{transientErr, dlqPublishErr},
			expectedErrText:       "publish inbound dead-letter message",
			expectedFailureReason: "dispatch_failed",
			expectedCorrelation:   "corr-3",
		},
		{
			name: "context canceled is not moved to dlq",
			delivery: amqp.Delivery{
				Body: []byte(`{"correlation_key":"corr-4","value":"x"}`),
			},
			sendMessage: func(int) error {
				return context.Canceled
			},
			expectedAttempts:     1,
			expectedPublishCalls: 0,
			expectedErrIs:        []error{context.Canceled},
			expectedErrText:      "send inbound message",
		},
	}

	for _, tc := range tests {

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()

			var sendAttempts atomic.Int64
			publishCalls := make([]struct {
				queueName string
				message   amqp.Publishing
			}, 0)

			svc := &messagingService{
				engine: &engineEventBusStub{
					sendMessage: func(context.Context, uuid.UUID, string, string, map[string]any) error {
						sendAttempts.Add(1)
						if tc.sendMessage == nil {
							return nil
						}

						return tc.sendMessage(int(sendAttempts.Load()))
					},
				},
				sleep: func(context.Context, time.Duration) error {
					return nil
				},
				jitter: func(time.Duration) time.Duration {
					return 0
				},
			}

			_, err := svc.processInboundDelivery(ctx, testProjectID, "incoming-queue", "incoming-queue.dlq", "message.name", tc.delivery, func(_ context.Context, queueName string, message amqp.Publishing) error {
				publishCalls = append(publishCalls, struct {
					queueName string
					message   amqp.Publishing
				}{
					queueName: queueName,
					message:   message,
				})

				return tc.publishErr
			})

			if len(tc.expectedErrIs) == 0 && tc.expectedErrText == "" && err != nil {
				t.Fatalf("expected nil error, got %v", err)
			}

			if tc.expectedErrText != "" && err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.expectedErrText)
			}

			for _, expectedErr := range tc.expectedErrIs {
				if err == nil || !errors.Is(err, expectedErr) {
					t.Fatalf("expected error to match %v, got %v", expectedErr, err)
				}
			}

			if tc.expectedErrText != "" && (err == nil || !strings.Contains(err.Error(), tc.expectedErrText)) {
				t.Fatalf("expected error text to include %q, got %v", tc.expectedErrText, err)
			}

			if got := int(sendAttempts.Load()); got != tc.expectedAttempts {
				t.Fatalf("expected %d send attempts, got %d", tc.expectedAttempts, got)
			}

			if len(publishCalls) != tc.expectedPublishCalls {
				t.Fatalf("expected %d DLQ publishes, got %d", tc.expectedPublishCalls, len(publishCalls))
			}

			if tc.expectedPublishCalls == 0 {
				return
			}

			if publishCalls[0].queueName != "incoming-queue.dlq" {
				t.Fatalf("expected publish queue %q, got %q", "incoming-queue.dlq", publishCalls[0].queueName)
			}

			var dlqPayload map[string]any
			if err := json.Unmarshal(publishCalls[0].message.Body, &dlqPayload); err != nil {
				t.Fatalf("failed to unmarshal DLQ payload: %v", err)
			}

			failureReason, _ := dlqPayload["failure_reason"].(string)
			if failureReason != tc.expectedFailureReason {
				t.Fatalf("expected failure_reason %q, got %q", tc.expectedFailureReason, failureReason)
			}

			correlationKey, _ := dlqPayload["correlation_key"].(string)
			if correlationKey != tc.expectedCorrelation {
				t.Fatalf("expected correlation_key %q, got %q", tc.expectedCorrelation, correlationKey)
			}

			originalQueue, _ := dlqPayload["original_queue"].(string)
			if originalQueue != "incoming-queue" {
				t.Fatalf("expected original_queue %q, got %q", "incoming-queue", originalQueue)
			}
		})
	}
}

// Losing a message the dead-letter queue would not take.
//
// The consumer acknowledged every message the moment the broker handed it over,
// so the dead-letter publish below was standing in for a message the broker had
// already forgotten. When that publish failed, nothing anywhere had the
// message: not the queue, not the DLQ, not the engine.
func TestAMessageTheDeadLetterQueueRefusesIsNotAcknowledged(t *testing.T) {
	t.Parallel()

	svc := &messagingService{
		engine: &engineEventBusStub{
			sendMessage: func(context.Context, uuid.UUID, string, string, map[string]any) error {
				return errors.New("no process is listening")
			},
		},
		sleep:                  func(context.Context, time.Duration) error { return nil },
		jitter:                 func(time.Duration) time.Duration { return 0 },
		inboundDispatchTimeout: time.Second,
	}

	outcome, err := svc.processInboundDelivery(
		t.Context(),
		uuid.New(),
		"incoming-queue",
		"incoming-queue.dlq",
		"message.name",
		amqp.Delivery{Body: []byte(`{"correlation_key":"corr-1","value":"x"}`)},
		func(context.Context, string, amqp.Publishing) error {
			return errors.New("the broker refused the dead-letter message")
		},
	)

	if err == nil {
		t.Fatal("a dispatch failure whose dead-letter publish also failed reported success")
	}
	if outcome != requeueDelivery {
		t.Fatalf("the message was acknowledged (outcome %v) after the dead-letter queue refused it; "+
			"nothing would have been holding it", outcome)
	}
}

// The counterpart: once the dead-letter queue has it, the original is accounted
// for and must not come back. Requeueing it would loop it forever.
func TestAMessageParkedInTheDeadLetterQueueIsAcknowledged(t *testing.T) {
	t.Parallel()

	svc := &messagingService{
		engine: &engineEventBusStub{
			sendMessage: func(context.Context, uuid.UUID, string, string, map[string]any) error {
				return errors.New("no process is listening")
			},
		},
		sleep:                  func(context.Context, time.Duration) error { return nil },
		jitter:                 func(time.Duration) time.Duration { return 0 },
		inboundDispatchTimeout: time.Second,
	}

	parked := 0
	outcome, err := svc.processInboundDelivery(
		t.Context(),
		uuid.New(),
		"incoming-queue",
		"incoming-queue.dlq",
		"message.name",
		amqp.Delivery{Body: []byte(`{"correlation_key":"corr-2","value":"x"}`)},
		func(context.Context, string, amqp.Publishing) error {
			parked++
			return nil
		},
	)

	if err == nil {
		t.Fatal("a dispatch failure reported success")
	}
	if parked != 1 {
		t.Fatalf("the message was published to the dead-letter queue %d times, want once", parked)
	}
	if outcome != ackDelivery {
		t.Fatalf("a message safely parked in the dead-letter queue was requeued (outcome %v); "+
			"it would come back forever", outcome)
	}
}

// A body nothing can parse, which the dead-letter queue then refuses too.
func TestAnUnreadableMessageTheDeadLetterQueueRefusesIsNotAcknowledged(t *testing.T) {
	t.Parallel()

	svc := &messagingService{
		sleep:                  func(context.Context, time.Duration) error { return nil },
		jitter:                 func(time.Duration) time.Duration { return 0 },
		inboundDispatchTimeout: time.Second,
	}

	outcome, err := svc.processInboundDelivery(
		t.Context(),
		uuid.New(),
		"incoming-queue",
		"incoming-queue.dlq",
		"message.name",
		amqp.Delivery{Body: []byte(`{not json`)},
		func(context.Context, string, amqp.Publishing) error {
			return errors.New("the broker refused the dead-letter message")
		},
	)

	if err == nil {
		t.Fatal("an unreadable body whose dead-letter publish also failed reported success")
	}
	if outcome != requeueDelivery {
		t.Fatalf("an unreadable message was acknowledged (outcome %v) with nowhere holding it", outcome)
	}
}

// A broker message that carried no correlation key.
//
// To the engine an empty key means every instance waiting on the message's
// name, so a message that merely lacked the field advanced all of them at once.
// It may still start a process — that is what a message start event is for —
// but it must reach no waiting instance.
func TestAMessageWithNoCorrelationKeyStartsProcessesButReachesNoWaitingInstance(t *testing.T) {
	t.Parallel()

	var sends, starts atomic.Int64
	svc := &messagingService{
		engine: &engineEventBusStub{
			sendMessage: func(context.Context, uuid.UUID, string, string, map[string]any) error {
				sends.Add(1)
				return nil
			},
			startFromMessage: func(context.Context, uuid.UUID, string, map[string]any) (int, error) {
				starts.Add(1)
				return 1, nil
			},
		},
		sleep:                  func(context.Context, time.Duration) error { return nil },
		jitter:                 func(time.Duration) time.Duration { return 0 },
		inboundDispatchTimeout: time.Second,
	}

	outcome, err := svc.processInboundDelivery(t.Context(), uuid.New(), "incoming-queue", "incoming-queue.dlq", "message.name",
		amqp.Delivery{Body: []byte(`{"value":"x"}`), Headers: amqp.Table{"correlation_key": ""}},
		func(context.Context, string, amqp.Publishing) error {
			t.Fatal("a message that started a process was dead-lettered")
			return nil
		})
	if err != nil {
		t.Fatalf("processInboundDelivery: %v", err)
	}
	if outcome != ackDelivery {
		t.Fatalf("outcome = %v, want the message acknowledged", outcome)
	}
	if got := sends.Load(); got != 0 {
		t.Fatalf("the message was sent to waiting instances %d times; it names none of them", got)
	}
	if got := starts.Load(); got != 1 {
		t.Fatalf("the message was offered to start events %d times, want 1", got)
	}
}

// A keyless message that no process starts on has reached nobody. It used to
// be acknowledged as if it had been delivered; now it is parked with the reason.
func TestAMessageWithNoCorrelationKeyThatStartsNothingIsDeadLetteredOnce(t *testing.T) {
	t.Parallel()

	var starts atomic.Int64
	svc := &messagingService{
		engine: &engineEventBusStub{
			sendMessage: func(context.Context, uuid.UUID, string, string, map[string]any) error {
				t.Fatal("a message with no correlation key was sent to waiting instances")
				return nil
			},
			startFromMessage: func(context.Context, uuid.UUID, string, map[string]any) (int, error) {
				starts.Add(1)
				return 0, nil
			},
		},
		sleep:                  func(context.Context, time.Duration) error { return nil },
		jitter:                 func(time.Duration) time.Duration { return 0 },
		inboundDispatchTimeout: time.Second,
	}

	var parked []amqp.Publishing
	outcome, err := svc.processInboundDelivery(t.Context(), uuid.New(), "incoming-queue", "incoming-queue.dlq", "message.name",
		amqp.Delivery{Body: []byte(`{"value":"x"}`)},
		func(_ context.Context, _ string, message amqp.Publishing) error {
			parked = append(parked, message)
			return nil
		})
	if !errors.Is(err, errInboundMessageReachedNobody) {
		t.Fatalf("err = %v, want it to say the message reached nobody", err)
	}
	if outcome != ackDelivery {
		t.Fatalf("outcome = %v, want the parked message acknowledged", outcome)
	}
	if got := starts.Load(); got != 1 {
		t.Fatalf("starting from the message was tried %d times; trying again changes nothing", got)
	}
	if len(parked) != 1 {
		t.Fatalf("dead-lettered %d times, want once", len(parked))
	}
	var body map[string]any
	if err := json.Unmarshal(parked[0].Body, &body); err != nil {
		t.Fatalf("dead-letter body: %v", err)
	}
	if body["failure_reason"] != "no_correlation_key" {
		t.Fatalf("failure_reason = %v, want no_correlation_key", body["failure_reason"])
	}
}

// TestARedeliveredMessageThatTimesOutAgainIsDeadLettered: a dispatch that ran
// out of its own time was requeued at once and every time, so a message that
// always took longer than the budget looped forever and held its queue behind
// it. The second time it runs out, it goes to the dead-letter queue.
func TestARedeliveredMessageThatTimesOutAgainIsDeadLettered(t *testing.T) {
	t.Parallel()

	slow := &messagingService{
		engine: &engineEventBusStub{
			sendMessage: func(ctx context.Context, _ uuid.UUID, _ string, _ string, _ map[string]any) error {
				<-ctx.Done()
				return ctx.Err()
			},
		},
		sleep:                  func(context.Context, time.Duration) error { return nil },
		jitter:                 func(time.Duration) time.Duration { return 0 },
		inboundDispatchTimeout: 20 * time.Millisecond,
	}
	deliver := func(ctx context.Context) (deliveryOutcome, int) {
		published := 0
		outcome, _ := slow.processInboundDelivery(ctx, uuid.New(), "q", "q.dlq", "message.name",
			amqp.Delivery{Redelivered: true, Body: []byte(`{"correlation_key":"k"}`)},
			func(context.Context, string, amqp.Publishing) error { published++; return nil })
		return outcome, published
	}

	if outcome, published := deliver(t.Context()); outcome != ackDelivery || published != 1 {
		t.Fatalf("outcome %v, %d dead letters; want it acknowledged and dead-lettered once", outcome, published)
	}

	// Stopping is still not the message's fault: it goes back.
	stopped, cancel := context.WithCancel(t.Context())
	cancel()
	if outcome, published := deliver(stopped); outcome != requeueDelivery || published != 0 {
		t.Fatalf("at shutdown: outcome %v, %d dead letters; want it requeued", outcome, published)
	}
}
