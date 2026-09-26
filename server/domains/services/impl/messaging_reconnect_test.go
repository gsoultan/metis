package impl

import (
	"context"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

// Reconnecting to a broker that is down. It was every 5 seconds, for as long
// as the broker stayed down: a broker gone for a weekend was dialled some
// fifty thousand times by every bridge and consumer of every replica. The
// wait now doubles from 5 seconds to 5 minutes, each one varied by a quarter
// so replicas do not come back in step, and starts over once connected.

var errConnectionRefused = errors.New("dial tcp 127.0.0.1:5672: connect: connection refused")

// scheduledWait is the wait the reconnect schedule gives before attempt
// number attempt, without its jitter: 5s, doubling, at most 5 minutes.
func scheduledWait(attempt int) time.Duration {
	return min(5*time.Second<<(attempt-1), 5*time.Minute)
}

// assertBackoff checks that a wait is the schedule's for attempt, give or
// take its jitter.
func assertBackoff(t *testing.T, wait time.Duration, attempt int) {
	t.Helper()
	want := scheduledWait(attempt)
	low, high := want-want/4, want+want/4
	if wait < low || wait > high {
		t.Fatalf("after %d failed attempts it waited %v, want %v give or take a quarter (%v to %v)",
			attempt, wait, want, low, high)
	}
}

// takeWait takes the next wait from the recorder, failing the test if none
// comes.
func takeWait(t *testing.T, waits waitRecorder) time.Duration {
	t.Helper()
	select {
	case wait := <-waits:
		return wait
	case <-time.After(5 * time.Second):
		t.Fatal("no wait came within 5s")
		return 0
	}
}

// nextBackoff takes waits until one is not the poll interval, and returns it:
// a round may run on a connection that has gone before the broker's word of
// it arrives.
func nextBackoff(t *testing.T, waits waitRecorder, pollInterval time.Duration) time.Duration {
	t.Helper()
	var seen []time.Duration
	for range 10 {
		wait := takeWait(t, waits)
		if wait != pollInterval {
			return wait
		}
		seen = append(seen, wait)
	}
	t.Fatalf("waited %v; the poll interval every time, never a wait to reconnect", seen)
	return 0
}

// runBridge runs bridge until the test ends.
func runBridge(t *testing.T, bridge *externalTaskBridge) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		bridge.run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})
}

func TestABridgeWhoseBrokerIsDownWaitsLongerEachTimeAndStartsOverOnceItConnects(t *testing.T) {
	t.Parallel()
	var logs lockedBuffer
	broker := newFakeBroker()
	const down = 8 // enough to reach the ceiling and stay there
	broker.failDials(slices.Repeat([]error{errConnectionRefused}, down)...)
	board, _ := newTaskBoard(0)
	bridge := bridgeOn(t, broker, board, &logs, time.Second)
	bridge.pollInterval = time.Second
	waits := make(waitRecorder)
	bridge.sleep = waits.sleep
	runBridge(t, bridge)

	if first := takeWait(t, waits); first != time.Second {
		t.Fatalf("the bridge waited %v before its first round, want its poll interval", first)
	}
	for attempt := 1; attempt <= down; attempt++ {
		assertBackoff(t, takeWait(t, waits), attempt)
	}
	if polling := takeWait(t, waits); polling != time.Second {
		t.Fatalf("connected, the bridge waited %v before its next round, want its poll interval", polling)
	}

	// The broker goes away again, and the schedule starts over.
	broker.failDials(errConnectionRefused)
	broker.dropConnection(&amqp.Error{Code: amqp.ConnectionForced, Reason: "CONNECTION_FORCED - shutdown", Server: true})
	assertBackoff(t, nextBackoff(t, waits, time.Second), 1)
}

func TestAConsumerWhoseBrokerIsDownWaitsLongerEachTimeAndStartsOverOnceItConsumes(t *testing.T) {
	t.Parallel()
	test := &consumerTest{broker: newFakeBroker(), waits: make(waitRecorder)}
	const down = 8
	test.broker.failDials(slices.Repeat([]error{errConnectionRefused}, down)...)
	runConsumer(t, test)

	for attempt := 1; attempt <= down; attempt++ {
		assertBackoff(t, takeWait(t, test.waits), attempt)
	}

	// Connected and consuming; then the broker closes the channel, and the
	// schedule starts over.
	consuming := test.awaitConsuming(t, 0)
	consuming.closeWith(&amqp.Error{Code: amqp.InternalError, Reason: "INTERNAL_ERROR", Server: true})
	assertBackoff(t, takeWait(t, test.waits), 1)
}

// Waiting to reconnect still ends the moment the server stops: a wait of
// minutes must not hold up a shutdown.
func TestWaitingToReconnectEndsWhenTheServerStops(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		run  func(ctx context.Context, svc *messagingService)
	}{
		{
			name: "bridge",
			run: func(ctx context.Context, svc *messagingService) {
				svc.newBridge(ctx, uuid.New(), "reverse-charge", "amqp://broker.test/", "billing", "charges.reverse").run(ctx)
			},
		},
		{
			name: "consumer",
			run: func(ctx context.Context, svc *messagingService) {
				svc.newConsumer(ctx, uuid.New(), "amqp://broker.test/", "orders", "OrderPaid").run(ctx)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			broker := newFakeBroker()
			broker.failDials(slices.Repeat([]error{errConnectionRefused}, 100)...)
			board, _ := newTaskBoard(0)
			svc := &messagingService{
				externalSvc:  board,
				dial:         broker.dial,
				pollInterval: 10 * time.Millisecond,
				sleep:        sleepWithContext,
			}
			logger := zerolog.New(io.Discard)
			ctx, cancel := context.WithCancel(logger.WithContext(t.Context()))
			stopped := make(chan struct{})
			go func() {
				defer close(stopped)
				tc.run(ctx, svc)
			}()
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
				if dials, _ := broker.counts(); dials > 0 {
					break
				}
			}
			cancel()
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("a second after the server stopped, it was still waiting to reconnect")
			}
		})
	}
}
