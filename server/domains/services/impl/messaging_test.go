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
	sendMessage func(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string, vars map[string]any) error
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

	err := svc.processInboundDelivery(
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

			err := svc.processInboundDelivery(ctx, testProjectID, "incoming-queue", "incoming-queue.dlq", "message.name", tc.delivery, func(_ context.Context, queueName string, message amqp.Publishing) error {
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
