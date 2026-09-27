package impl

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync"
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
// so replicas do not come back in step, and starts over once a connection
// has lasted. A broker that takes each connection and drops it straight away
// is waited for as if it were down.

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

// steppedWaits stands in for a bridge's sleep and holds the bridge between
// rounds: each wait goes to the test, and the bridge stays in it until the
// test asks for the next. A test changes the broker while the bridge is
// held, so the change cannot race the round it is meant to come before.
//
// It replaces a recorder that let the bridge go the moment the test took a
// wait. The test's next step then raced the bridge's next round, and making
// the broker reachable nearly always beat the dial it was meant to follow:
// TestABridgeBacksOffFromABrokerItCannotReachAndStartsOverOnceItConnects
// failed in CI with "after 3 failed attempts it waited 1s", a bridge that had
// connected, as it should have, a round early.
type steppedWaits struct {
	waits   chan time.Duration
	release chan struct{}
	holding bool // the bridge is held in a wait; the test's alone
}

func newSteppedWaits() *steppedWaits {
	return &steppedWaits{waits: make(chan time.Duration), release: make(chan struct{})}
}

func (s *steppedWaits) sleep(ctx context.Context, delay time.Duration) error {
	select {
	case s.waits <- delay:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// next lets the bridge go from the wait it is held in, if any, and returns
// the next one, holding the bridge in it.
func (s *steppedWaits) next(t *testing.T) time.Duration {
	t.Helper()
	if s.holding {
		select {
		case s.release <- struct{}{}:
		case <-time.After(5 * time.Second):
			t.Fatal("the bridge was not waiting to be let go")
		}
	}
	select {
	case wait := <-s.waits:
		s.holding = true
		return wait
	case <-time.After(5 * time.Second):
		t.Fatal("no wait came within 5s")
		return 0
	}
}

// nextBackoff takes waits until one is not the poll interval, and returns it.
// A round or two can run on a connection a proxy has cut before the AMQP
// library has read the cut, so it pauses between them, with the bridge held.
func nextBackoff(t *testing.T, waits *steppedWaits, pollInterval time.Duration) time.Duration {
	t.Helper()
	var seen []time.Duration
	for range 50 {
		wait := waits.next(t)
		if wait != pollInterval {
			return wait
		}
		seen = append(seen, wait)
		time.Sleep(20 * time.Millisecond)
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

// The path the broker-backed test runs through a proxy, against the broker in
// memory: refusing, then reachable, a connection that lasts, then cut and
// refusing again. Every change is made while the bridge is held.
func TestABridgeWhoseBrokerIsDownWaitsLongerEachTimeAndStartsOverOnceAConnectionLasts(t *testing.T) {
	t.Parallel()
	var logs lockedBuffer
	broker := newFakeBroker()
	broker.refuse(true)
	board, _ := newTaskBoard(0)
	bridge := bridgeOn(t, broker, board, &logs, time.Second)
	bridge.pollInterval = time.Second
	waits := newSteppedWaits()
	bridge.sleep = waits.sleep
	runBridge(t, bridge)

	if first := waits.next(t); first != time.Second {
		t.Fatalf("the bridge waited %v before its first round, want its poll interval", first)
	}
	const down = 8 // enough to reach the ceiling and stay there
	for attempt := 1; attempt <= down; attempt++ {
		assertBackoff(t, waits.next(t), attempt)
	}

	broker.refuse(false)
	if connected := waits.next(t); connected != time.Second {
		t.Fatalf("connected, the bridge waited %v before its next round, want its poll interval", connected)
	}
	if lasted := waits.next(t); lasted != time.Second {
		t.Fatalf("its connection still up, the bridge waited %v, want its poll interval", lasted)
	}

	// The broker goes away again, and the schedule starts over.
	broker.refuse(true)
	broker.dropConnection(&amqp.Error{Code: amqp.ConnectionForced, Reason: "CONNECTION_FORCED - shutdown", Server: true})
	assertBackoff(t, waits.next(t), 1)
}

// A broker that takes each connection and drops it before the bridge's next
// round is not a broker that is up. Each such connection used to count as a
// success, so the bridge dialled it again at every round, 5 seconds apart,
// for as long as it went on. A connection counts once it has lasted a round
// or carried a task; one lost before then is a failed attempt.
func TestABridgeWhoseBrokerDropsEachConnectionWaitsLongerEachTime(t *testing.T) {
	t.Parallel()
	var logs lockedBuffer
	broker := newFakeBroker()
	board, _ := newTaskBoard(0)
	bridge := bridgeOn(t, broker, board, &logs, time.Second)
	bridge.pollInterval = time.Second
	waits := newSteppedWaits()
	bridge.sleep = waits.sleep
	runBridge(t, bridge)

	if first := waits.next(t); first != time.Second {
		t.Fatalf("the bridge waited %v before its first round, want its poll interval", first)
	}
	for attempt := 1; attempt <= 4; attempt++ {
		if connected := waits.next(t); connected != time.Second {
			t.Fatalf("connected, the bridge waited %v before its next round, want its poll interval", connected)
		}
		broker.dropConnection(&amqp.Error{Code: amqp.ConnectionForced, Reason: "CONNECTION_FORCED - closed by a proxy", Server: true})
		assertBackoff(t, waits.next(t), attempt)
	}
	if lost := errorLines(t, &logs, "CONNECTION_FORCED"); len(lost) == 0 {
		t.Errorf("a connection dropped before it had lasted was not said at all: %v", logs.entries(t))
	}
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

// The consumer's counterpart: a session whose connection the broker drops as
// soon as it is consuming counted as a success, and the consumer came back
// after 5 seconds, every time. A session counts once it has lasted the
// schedule's first wait, or ended with its connection still up.
func TestAConsumerWhoseBrokerDropsEachConnectionWaitsLongerEachTime(t *testing.T) {
	t.Parallel()
	test := &consumerTest{broker: newFakeBroker(), waits: make(waitRecorder)}
	runConsumer(t, test)

	for attempt := 1; attempt <= 4; attempt++ {
		test.awaitConsuming(t, attempt-1)
		test.broker.dropConnection(&amqp.Error{Code: amqp.ConnectionForced, Reason: "CONNECTION_FORCED - closed by a proxy", Server: true})
		assertBackoff(t, takeWait(t, test.waits), attempt)
	}
}

// A session that lasted is a success however it ends: after a broker that was
// down for a while, one hour of consuming and then a lost connection starts
// the schedule over, rather than going on from where the outage left it.
func TestAConsumerWhoseConnectionLastedStartsOverWhenItIsLost(t *testing.T) {
	t.Parallel()
	clock := &fakeClock{now: time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)}
	test := &consumerTest{broker: newFakeBroker(), waits: make(waitRecorder), now: clock.read}
	test.broker.failDials(errConnectionRefused, errConnectionRefused, errConnectionRefused)
	runConsumer(t, test)

	for attempt := 1; attempt <= 3; attempt++ {
		assertBackoff(t, takeWait(t, test.waits), attempt)
	}
	test.awaitConsuming(t, 0)
	clock.advance(time.Hour)
	test.broker.dropConnection(&amqp.Error{Code: amqp.ConnectionForced, Reason: "CONNECTION_FORCED - shutdown", Server: true})
	assertBackoff(t, takeWait(t, test.waits), 1)
}

// fakeClock is a time a test moves on by hand.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) read() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(by time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(by)
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
				svc.newBridge(ctx, uuid.New(), "reverse-charge", "amqp://broker.test/", "billing", "charges.reverse", time.Minute).run(ctx)
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
