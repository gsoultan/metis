package impl

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

// The inbound consumer against a broker in memory: what it does when the
// broker closes its channel, cancels it, or does not confirm a message it
// parks on the dead-letter queue.

// waitRecorder stands in for the consumer's sleep: each wait goes to the test
// instead of the clock. Unbuffered, a wait lasts until the test takes it.
type waitRecorder chan time.Duration

func (w waitRecorder) sleep(ctx context.Context, delay time.Duration) error {
	select {
	case w <- delay:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// consumerTest is a consumer of the queue "orders" on a fake broker, running
// until the test ends, whose correlations the test can read.
type consumerTest struct {
	broker     *fakeBroker
	logs       lockedBuffer
	correlated chan string
	waits      waitRecorder
	dispatch   error            // what correlating a message returns
	now        func() time.Time // the consumer's clock, when not the real one
	stopped    chan struct{}
}

// runConsumer starts a consumer over the fake broker, as StartInboundConsumer
// assembles one, with waits taken by the recorder.
func runConsumer(t *testing.T, test *consumerTest) {
	t.Helper()
	test.correlated = make(chan string, 16)
	test.stopped = make(chan struct{})
	svc := &messagingService{
		engine: &engineEventBusStub{sendMessage: func(_ context.Context, _ uuid.UUID, _, key string, _ map[string]any) error {
			test.correlated <- key
			return test.dispatch
		}},
		dial:                   test.broker.dial,
		confirmTimeout:         50 * time.Millisecond,
		sleep:                  func(context.Context, time.Duration) error { return nil },
		jitter:                 func(time.Duration) time.Duration { return 0 },
		inboundDispatchTimeout: time.Second,
	}
	logger := zerolog.New(&test.logs)
	ctx, cancel := context.WithCancel(logger.WithContext(t.Context()))
	consumer := svc.newConsumer(ctx, uuid.New(), "amqp://broker.test/", "orders", "OrderPaid")
	consumer.sleep = test.waits.sleep
	if test.now != nil {
		consumer.now = test.now
	}
	go func() {
		defer close(test.stopped)
		consumer.run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-test.stopped
	})
}

// awaitConsuming waits until the nth channel opened has a consumer on it.
func (c *consumerTest) awaitConsuming(t *testing.T, n int) *fakeChannel {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if _, channels := c.broker.counts(); channels > n {
			ch := c.broker.channel(n)
			ch.broker.mu.Lock()
			consuming := ch.deliveries != nil
			ch.broker.mu.Unlock()
			if consuming {
				return ch
			}
		}
	}
	t.Fatalf("5s on, nothing consumes on channel %d (dials and channels so far: %v)", n, fmtCounts(c.broker))
	return nil
}

// awaitCorrelated waits for a message with correlation key to be correlated.
func (c *consumerTest) awaitCorrelated(t *testing.T, key string) {
	t.Helper()
	select {
	case got := <-c.correlated:
		if got != key {
			t.Fatalf("correlated %q, want %q", got, key)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("the message for %q was not correlated within 5s", key)
	}
}

func fmtCounts(broker *fakeBroker) []int {
	dials, channels := broker.counts()
	return []int{dials, channels}
}

// A channel the broker closes ended the consumer's deliveries in silence: it
// dialled a new connection at once, and nothing said why it had stopped. Now
// it says why, once, and consumes again on a new channel of the connection it
// has.
func TestAConsumerConsumesAgainOnANewChannelWhenItsBrokerClosesOne(t *testing.T) {
	t.Parallel()
	test := &consumerTest{broker: newFakeBroker(), waits: make(waitRecorder, 16)}
	runConsumer(t, test)

	first := test.awaitConsuming(t, 0)
	first.deliver(`{"correlation_key":"order-1"}`)
	test.awaitCorrelated(t, "order-1")
	first.closeWith(&amqp.Error{Code: amqp.PreconditionFailed, Reason: "PRECONDITION_FAILED - unknown delivery tag 7", Server: true})

	second := test.awaitConsuming(t, 1)
	second.deliver(`{"correlation_key":"order-2"}`)
	test.awaitCorrelated(t, "order-2")

	if dials, _ := test.broker.counts(); dials != 1 {
		t.Errorf("%d dials; the connection was still up, so only the channel should have been replaced", dials)
	}
	if declared := test.broker.declaredQueues(); !slices.Equal(declared, []string{"orders", "orders.dlq", "orders", "orders.dlq"}) {
		t.Errorf("declared %v, want the queue and its dead-letter queue on each channel", declared)
	}
	why := errorLines(t, &test.logs, "PRECONDITION_FAILED")
	if len(why) != 1 {
		t.Fatalf("the closed channel was logged at error %d times, want once: %v", len(why), test.logs.entries(t))
	}
	assertNamed(t, why[0], map[string]string{"queue": "orders", "messageName": "OrderPaid"})
}

// The broker cancels a consumer whose queue is deleted. The channel stays open
// and the deliveries simply stop; the consumer now says so, and consumes
// again on a new channel, declaring the queue afresh.
func TestAConsumerTheBrokerCancelsConsumesAgain(t *testing.T) {
	t.Parallel()
	test := &consumerTest{broker: newFakeBroker(), waits: make(waitRecorder, 16)}
	runConsumer(t, test)

	first := test.awaitConsuming(t, 0)
	first.cancelConsumer()
	second := test.awaitConsuming(t, 1)
	second.deliver(`{"correlation_key":"order-3"}`)
	test.awaitCorrelated(t, "order-3")

	if dials, _ := test.broker.counts(); dials != 1 {
		t.Errorf("%d dials; a cancelled consumer is consumed again on the same connection", dials)
	}
	if !first.isClosed() {
		t.Error("the channel whose consumer was cancelled was left open")
	}
	if why := errorLines(t, &test.logs, "cancelled"); len(why) != 1 {
		t.Fatalf("the cancel was logged at error %d times, want once: %v", len(why), test.logs.entries(t))
	}
}

// A dead-letter publish the broker does not confirm leaves the message
// requeued, and the channel is not used again: the answer about the lost
// publish could still come, and be read as the answer about the next.
func TestAConsumerReplacesItsChannelWhenADeadLetterPublishIsNotConfirmed(t *testing.T) {
	t.Parallel()
	test := &consumerTest{broker: newFakeBroker(), waits: make(waitRecorder, 16), dispatch: errors.New("no process is listening")}
	test.broker.answer(answerNever)
	runConsumer(t, test)

	first := test.awaitConsuming(t, 0)
	first.deliver(`{"correlation_key":"order-4"}`)
	test.awaitConsuming(t, 1)

	if settled := test.broker.settlements(); !slices.Equal(settled, []string{"requeue"}) {
		t.Errorf("the message was settled %v, want requeued: the dead-letter queue never said it had it", settled)
	}
	if !first.isClosed() {
		t.Error("the channel that missed a confirm is still open")
	}
	if dials, _ := test.broker.counts(); dials != 1 {
		t.Errorf("%d dials; a missed confirm replaces the channel, not the connection", dials)
	}
}
