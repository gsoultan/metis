package impl

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

// A bridge or consumer that could not reach its broker said "Bridge RabbitMQ
// connection error" or "Consumer error; retrying", and nothing else. With more
// than one configured, nobody reading the log could tell which one had lost its
// broker, and one that came back said nothing at all. Each now names itself in
// every line, through the logger it was started with, and says when it has
// connected.

// lockedBuffer is a log destination the bridge's goroutine writes while the
// test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// entries returns what has been logged so far, one map per line.
func (b *lockedBuffer) entries(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	written := b.buf.String()
	b.mu.Unlock()

	var entries []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(written))
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("a log line is not JSON: %q", scanner.Text())
		}
		entries = append(entries, entry)
	}
	return entries
}

// awaitEntry waits for the first line at level whose message contains words.
func awaitEntry(t *testing.T, logs *lockedBuffer, level, words string, within time.Duration) map[string]any {
	t.Helper()
	for deadline := time.Now().Add(within); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		for _, entry := range logs.entries(t) {
			message, _ := entry["message"].(string)
			if entry["level"] == level && strings.Contains(message, words) {
				return entry
			}
		}
	}
	t.Fatalf("nothing at level %q saying %q reached the logger it was started with within %s; it logged %v",
		level, words, within, logs.entries(t))
	return nil
}

// refusedBroker is a broker URL nothing listens on, so a dial fails at once
// rather than after a connection timeout.
func refusedBroker(t *testing.T) string {
	t.Helper()
	var config net.ListenConfig
	listener, err := config.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port: %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("free the port: %v", err)
	}
	return "amqp://guest:guest@" + address + "/"
}

// assertNamed checks that a line carries every field that says which bridge or
// consumer wrote it.
func assertNamed(t *testing.T, entry map[string]any, fields map[string]string) {
	t.Helper()
	for field, want := range fields {
		if got := entry[field]; got != want {
			t.Errorf("%s = %v, want %q: the line does not say which one it is: %v", field, got, want, entry)
		}
	}
}

func TestAConsumerThatCannotReachItsBrokerSaysWhichConsumerItIs(t *testing.T) {
	t.Parallel()

	var logs lockedBuffer
	logger := zerolog.New(&logs)
	svc := NewMessagingService(&engineEventBusStub{}, nil)
	t.Cleanup(svc.StopAll)

	project := uuid.New()
	ctx := logger.WithContext(t.Context())
	if err := svc.StartInboundConsumer(ctx, project, refusedBroker(t), "orders", "OrderPaid"); err != nil {
		t.Fatalf("start the consumer: %v", err)
	}

	entry := awaitEntry(t, &logs, "error", "", 5*time.Second)
	assertNamed(t, entry, map[string]string{
		"project":     project.String(),
		"queue":       "orders",
		"messageName": "OrderPaid",
	})
}

func TestABridgeThatCannotReachItsBrokerSaysWhichBridgeItIs(t *testing.T) {
	t.Parallel()

	var logs lockedBuffer
	logger := zerolog.New(&logs)
	svc := NewMessagingService(&engineEventBusStub{}, &externalTaskStub{})
	t.Cleanup(svc.StopAll)

	project := uuid.New()
	ctx := logger.WithContext(t.Context())
	if err := svc.StartBridge(ctx, project, "reverse-charge", refusedBroker(t), "billing", "charges.reverse"); err != nil {
		t.Fatalf("start the bridge: %v", err)
	}

	// The bridge dials on its first poll, not at once.
	entry := awaitEntry(t, &logs, "error", "", bridgePollInterval+5*time.Second)
	assertNamed(t, entry, map[string]string{
		"project":    project.String(),
		"topic":      "reverse-charge",
		"exchange":   "billing",
		"routingKey": "charges.reverse",
	})
}

// The two below need a broker to connect to, and run where one is configured,
// as the connector's suite does.

const testBrokerURLEnv = "METIS_TEST_RABBITMQ_URL"

func testBrokerURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv(testBrokerURLEnv)
	if url == "" {
		t.Skipf("set %s to run this against a live RabbitMQ broker", testBrokerURLEnv)
	}
	return url
}

// deleteQueues removes what a consumer declared, so a run leaves the broker as
// it found it.
func deleteQueues(t *testing.T, url string, queues ...string) {
	t.Helper()
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Errorf("dial the broker to clean up: %v", err)
		return
	}
	defer closeQuietly(conn, "AMQP connection")
	ch, err := conn.Channel()
	if err != nil {
		t.Errorf("open a channel to clean up: %v", err)
		return
	}
	defer closeQuietly(ch, "AMQP channel")
	for _, queue := range queues {
		if _, err := ch.QueueDelete(queue, false, false, false); err != nil {
			t.Errorf("delete %s: %v", queue, err)
		}
	}
}

func TestAConsumerSaysWhenItIsConsuming(t *testing.T) {
	url := testBrokerURL(t)

	var logs lockedBuffer
	logger := zerolog.New(&logs)
	svc := NewMessagingService(&engineEventBusStub{}, nil)
	queue := "metis-test-consumer-" + uuid.NewString()
	t.Cleanup(func() {
		svc.StopAll()
		deleteQueues(t, url, queue, queue+inboundDeadLetterQueueSuffix)
	})

	project := uuid.New()
	if err := svc.StartInboundConsumer(logger.WithContext(t.Context()), project, url, queue, "OrderPaid"); err != nil {
		t.Fatalf("start the consumer: %v", err)
	}

	entry := awaitEntry(t, &logs, "info", "consuming", 10*time.Second)
	assertNamed(t, entry, map[string]string{"project": project.String(), "queue": queue})
}

func TestABridgeSaysWhenItHasConnected(t *testing.T) {
	url := testBrokerURL(t)

	var logs lockedBuffer
	logger := zerolog.New(&logs)
	svc := NewMessagingService(&engineEventBusStub{}, &externalTaskStub{})
	t.Cleanup(svc.StopAll)

	project := uuid.New()
	if err := svc.StartBridge(logger.WithContext(t.Context()), project, "reverse-charge", url, "amq.direct", "charges.reverse"); err != nil {
		t.Fatalf("start the bridge: %v", err)
	}

	entry := awaitEntry(t, &logs, "info", "connected", bridgePollInterval+10*time.Second)
	assertNamed(t, entry, map[string]string{"project": project.String(), "topic": "reverse-charge"})
}

// externalTaskStub offers no work, which is all a bridge needs to poll.
type externalTaskStub struct{}

func (externalTaskStub) FetchAndLock(context.Context, string, string, int, int64) ([]*entities.ExternalTask, error) {
	return nil, nil
}

func (externalTaskStub) Complete(context.Context, uuid.UUID, string, map[string]any) error {
	return nil
}

func (externalTaskStub) HandleFailure(context.Context, uuid.UUID, string, string, string, int, int64) error {
	return nil
}

func (externalTaskStub) Create(context.Context, *entities.ExternalTask) error { return nil }
