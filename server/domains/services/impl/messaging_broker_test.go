package impl

import (
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

// The bridge, the consumer and the connector against a real broker, for what
// a broker in memory cannot vouch for: that the AMQP library behaves the way
// the fake does. Each needs a broker and skips without one; CI sets
// METIS_TEST_RABBITMQ_URL, and fails on any skip.

// consumeTestQueue declares a queue of the test's own on the broker, deleted
// when the test ends, and consumes it.
func consumeTestQueue(t *testing.T, url string) (string, <-chan amqp.Delivery) {
	t.Helper()
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("dial the broker: %v", err)
	}
	t.Cleanup(func() { closeQuietly(conn, "test connection") })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("open a channel: %v", err)
	}
	queue, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatalf("declare a queue: %v", err)
	}
	deliveries, err := ch.Consume(queue.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatalf("consume %s: %v", queue.Name, err)
	}
	return queue.Name, deliveries
}

// awaitDeliveryOf waits for the message carrying task id, passing over any
// other — including earlier copies, which at-least-once delivery allows.
func awaitDeliveryOf(t *testing.T, deliveries <-chan amqp.Delivery, id uuid.UUID, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case delivery := <-deliveries:
			if delivery.Headers["task_id"] == id.String() {
				return
			}
		case <-deadline:
			t.Fatalf("task %s did not reach the queue within %s", id, within)
		}
	}
}

// awaitHandedBack waits until the board has had task id handed back, and
// returns why.
func awaitHandedBack(t *testing.T, board *taskBoard, id uuid.UUID, within time.Duration) string {
	t.Helper()
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		handedBack, details := board.returned()
		if i := slices.Index(handedBack, id); i >= 0 {
			return details[i]
		}
	}
	t.Fatalf("task %s was not handed back within %s", id, within)
	return ""
}

// liveBridge starts a bridge on svc that publishes to queue through the
// default exchange, logging to logs, and stops it when the test ends.
func liveBridge(t *testing.T, svc *messagingService, url, queue string, logs *lockedBuffer) {
	t.Helper()
	logger := zerolog.New(logs)
	if err := svc.StartBridge(logger.WithContext(t.Context()), uuid.New(), "reverse-charge", url, "", queue); err != nil {
		t.Fatalf("start the bridge: %v", err)
	}
	awaitEntry(t, logs, "info", "connected", 10*time.Second)
}

// countingDial dials the broker and counts how often.
func countingDial(dials *atomic.Int32) func(string) (brokerConnection, error) {
	return func(url string) (brokerConnection, error) {
		dials.Add(1)
		return dialAMQP(url)
	}
}

// A broker that takes a publish and never confirms it held the bridge on that
// task for good. Now the task is handed back once the confirm is overdue, and
// the bridge goes on forwarding on a new channel of the same connection.
func TestABridgeHandsBackATaskARealBrokerDoesNotConfirmAndGoesOnForwarding(t *testing.T) {
	url := testBrokerURL(t)
	proxy, proxied := newBrokerProxy(t, url)
	queue, deliveries := consumeTestQueue(t, url)

	var logs lockedBuffer
	var dials atomic.Int32
	board, _ := newTaskBoard(0)
	svc := &messagingService{
		externalSvc:    board,
		dial:           countingDial(&dials),
		confirmTimeout: time.Second,
		pollInterval:   50 * time.Millisecond,
		sleep:          sleepWithContext,
	}
	t.Cleanup(svc.StopAll)
	liveBridge(t, svc, proxied, queue, &logs)

	proxy.swallowConfirms(true)
	unconfirmed := board.add()
	if why := awaitHandedBack(t, board, unconfirmed, 10*time.Second); !strings.Contains(why, "did not confirm") {
		t.Errorf("the task was handed back because %q, want the missing confirm named", why)
	}

	proxy.swallowConfirms(false)
	next := board.add()
	awaitDeliveryOf(t, deliveries, next, 10*time.Second)
	if n := dials.Load(); n != 1 {
		t.Errorf("the bridge dialled %d times; a missed confirm replaces the channel, not the connection", n)
	}
}

// The connector's wait had no deadline either, and a service task's context
// has none of its own: a broker that never confirmed held a job worker's slot
// for good.
func TestTheRabbitMQConnectorGivesUpOnAConfirmThatDoesNotCome(t *testing.T) {
	url := testBrokerURL(t)
	proxy, proxied := newBrokerProxy(t, url)
	queue, _ := consumeTestQueue(t, url)

	executor := NewRabbitMQExecutor()
	executor.confirmTimeout = time.Second
	config := map[string]any{"url": proxied, "queue": queue}
	proxy.swallowConfirms(true)

	published := make(chan error, 1)
	go func() {
		_, err := executor.Execute(t.Context(), config, map[string]any{"amount": 42})
		published <- err
	}()
	select {
	case err := <-published:
		if !errors.Is(err, errConfirmTimeout) {
			t.Fatalf("the publish returned %v, want the missing confirm", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("10s after publishing, the connector is still waiting for a confirm the broker never sent")
	}
}
