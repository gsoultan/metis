package impl

import (
	"context"
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

// awaitHandedBackTimes waits until the board has had task id handed back n
// times.
func awaitHandedBackTimes(t *testing.T, board *taskBoard, id uuid.UUID, n int, within time.Duration) {
	t.Helper()
	count := 0
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		handedBack, _ := board.returned()
		count = 0
		for _, returned := range handedBack {
			if returned == id {
				count++
			}
		}
		if count >= n {
			return
		}
	}
	t.Fatalf("task %s was handed back %d times within %s, want %d", id, count, within, n)
}

// liveBridge starts a bridge on svc that publishes to queue through the
// default exchange, logging to logs, and stops it when the test ends.
func liveBridge(t *testing.T, svc *messagingService, url, queue string, logs *lockedBuffer) {
	t.Helper()
	logger := zerolog.New(logs)
	if err := svc.StartBridge(logger.WithContext(t.Context()), uuid.New(), "reverse-charge", url, "", queue, time.Minute); err != nil {
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

// awaitLines waits for at least n lines at level whose message or error
// contains words, and returns them.
func awaitLines(t *testing.T, logs *lockedBuffer, level, words string, n int, within time.Duration) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		lines = lines[:0]
		for _, entry := range logs.entries(t) {
			message, _ := entry["message"].(string)
			reason, _ := entry["error"].(string)
			if entry["level"] == level && strings.Contains(message+" "+reason, words) {
				lines = append(lines, entry)
			}
		}
		if len(lines) >= n {
			return lines
		}
	}
	t.Fatalf("waited %s for %d lines at %s saying %q, and found %d: %v", within, n, level, words, len(lines), logs.entries(t))
	return nil
}

// A bridge whose exchange does not exist has its channel closed by the broker
// on the first publish. It used to stay closed until the server restarted, so
// creating the exchange changed nothing. Now the next round opens a new
// channel, and once the exchange exists the task goes out, on the same
// connection, with the reason said once however many rounds it recurred.
func TestABridgeWhoseExchangeIsMissingForwardsOnceItExistsWithoutARestart(t *testing.T) {
	url := testBrokerURL(t)
	exchange := "metis-test-" + uuid.NewString()

	var logs lockedBuffer
	var dials atomic.Int32
	board, _ := newTaskBoard(0)
	svc := &messagingService{
		externalSvc:    board,
		dial:           countingDial(&dials),
		confirmTimeout: 5 * time.Second,
		pollInterval:   50 * time.Millisecond,
		sleep:          sleepWithContext,
	}
	t.Cleanup(svc.StopAll)
	logger := zerolog.New(&logs)
	if err := svc.StartBridge(logger.WithContext(t.Context()), uuid.New(), "reverse-charge", url, exchange, "charges.reverse", time.Minute); err != nil {
		t.Fatalf("start the bridge: %v", err)
	}
	awaitEntry(t, &logs, "info", "connected", 10*time.Second)

	// Three rounds against the missing exchange, each closing a channel for
	// the same reason.
	task := board.add()
	awaitHandedBackTimes(t, board, task, 3, 10*time.Second)

	ch := brokerChannelFor(t, url)
	if err := ch.ExchangeDeclare(exchange, "direct", false, true, false, false, nil); err != nil {
		t.Fatalf("declare %s: %v", exchange, err)
	}
	queue, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatalf("declare a queue: %v", err)
	}
	if err := ch.QueueBind(queue.Name, "charges.reverse", exchange, false, nil); err != nil {
		t.Fatalf("bind %s to %s: %v", queue.Name, exchange, err)
	}
	deliveries, err := ch.Consume(queue.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatalf("consume %s: %v", queue.Name, err)
	}

	awaitDeliveryOf(t, deliveries, task, 10*time.Second)
	if n := dials.Load(); n != 1 {
		t.Errorf("the bridge dialled %d times; a closed channel is replaced on the connection it had", n)
	}
	if closed := awaitLines(t, &logs, "error", "NOT_FOUND", 1, time.Second); len(closed) != 1 {
		t.Errorf("the missing exchange was logged at error %d times, want once", len(closed))
	}
}

// brokerChannelFor opens a channel of the test's own on the broker, closed
// when the test ends.
func brokerChannelFor(t *testing.T, url string) *amqp.Channel {
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
	return ch
}

// Deleting a consumer's queue makes the broker cancel the consumer. The
// consumer used to dial a new connection at once without a word; now it says
// the broker cancelled it, declares the queue again on a new channel of the
// same connection, and correlates what arrives on it.
func TestAConsumerWhoseQueueIsDeletedSaysSoAndConsumesItAgain(t *testing.T) {
	url := testBrokerURL(t)
	queue := "metis-test-consumer-" + uuid.NewString()

	var logs lockedBuffer
	var dials atomic.Int32
	correlated := make(chan string, 4)
	svc := &messagingService{
		engine: &engineEventBusStub{sendMessage: func(_ context.Context, _ uuid.UUID, _, key string, _ map[string]any) error {
			correlated <- key
			return nil
		}},
		dial:                   countingDial(&dials),
		confirmTimeout:         5 * time.Second,
		sleep:                  sleepWithContext,
		jitter:                 randomJitter,
		inboundDispatchTimeout: 5 * time.Second,
	}
	t.Cleanup(func() {
		svc.StopAll()
		deleteQueues(t, url, queue, queue+inboundDeadLetterQueueSuffix)
	})
	logger := zerolog.New(&logs)
	if err := svc.StartInboundConsumer(logger.WithContext(t.Context()), uuid.New(), url, queue, "OrderPaid"); err != nil {
		t.Fatalf("start the consumer: %v", err)
	}
	awaitLines(t, &logs, "info", "consuming", 1, 10*time.Second)

	deleteQueues(t, url, queue)
	cancelled := awaitLines(t, &logs, "error", "cancelled", 1, 10*time.Second)
	assertNamed(t, cancelled[0], map[string]string{"queue": queue, "messageName": "OrderPaid"})
	awaitLines(t, &logs, "info", "consuming", 2, 20*time.Second)

	ch := brokerChannelFor(t, url)
	err := ch.PublishWithContext(t.Context(), "", queue, false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        []byte(`{"correlation_key":"order-9"}`),
	})
	if err != nil {
		t.Fatalf("publish to %s: %v", queue, err)
	}
	select {
	case key := <-correlated:
		if key != "order-9" {
			t.Fatalf("correlated %q, want order-9", key)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a message put on the queue after it was declared again was not correlated within 10s")
	}
	if n := dials.Load(); n != 1 {
		t.Errorf("the consumer dialled %d times; a cancelled consumer consumes again on the connection it has", n)
	}
}

// A broker that cannot be reached is tried again after a wait that grows with
// each attempt, and once reached, and the connection has lasted a round, the
// schedule starts over the next time it goes away. It was a flat 5 seconds
// for as long as the broker stayed down.
//
// The proxy is only ever changed while the bridge is held in a wait. It was
// changed while the bridge ran, so making the broker reachable raced the dial
// it was meant to follow, nearly always won, and this failed in CI with
// "after 3 failed attempts it waited 1s": a bridge that had connected, as it
// should have, a round early.
func TestABridgeBacksOffFromABrokerItCannotReachAndStartsOverOnceItConnects(t *testing.T) {
	url := testBrokerURL(t)
	proxy, proxied := newBrokerProxy(t, url)
	proxy.refuse(true)

	var logs lockedBuffer
	board, _ := newTaskBoard(0)
	svc := &messagingService{
		externalSvc:    board,
		dial:           dialAMQP,
		confirmTimeout: 5 * time.Second,
		pollInterval:   time.Second,
		sleep:          sleepWithContext,
	}
	logger := zerolog.New(&logs)
	bridge := svc.newBridge(logger.WithContext(t.Context()), uuid.New(), "reverse-charge", proxied, "", "metis-test-unused", time.Minute)
	waits := newSteppedWaits()
	bridge.sleep = waits.sleep
	runBridge(t, bridge)

	if first := waits.next(t); first != time.Second {
		t.Fatalf("the bridge waited %v before its first round, want its poll interval", first)
	}
	assertBackoff(t, waits.next(t), 1)
	assertBackoff(t, waits.next(t), 2)
	assertBackoff(t, waits.next(t), 3)

	// Held in its third wait, the bridge finds the broker reachable at the
	// round after it.
	proxy.refuse(false)
	if connected := waits.next(t); connected != time.Second {
		t.Fatalf("connected, the bridge waited %v before its next round, want its poll interval", connected)
	}
	awaitEntry(t, &logs, "info", "connected", 5*time.Second)
	if lasted := waits.next(t); lasted != time.Second {
		t.Fatalf("its connection still up, the bridge waited %v, want its poll interval", lasted)
	}

	// The broker goes away again, and the schedule starts over.
	proxy.refuse(true)
	proxy.sever()
	assertBackoff(t, nextBackoff(t, waits, time.Second), 1)
}
