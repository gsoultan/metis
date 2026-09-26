package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/rs/zerolog"
)

// The runner, against stand-ins for the database and the messaging service:
// what it reports when it cannot start something, how often it tries again,
// and what it hands the messaging service when it can.

// logRecorder is a log destination the runner's goroutines write while the
// test reads it.
type logRecorder struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logRecorder) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logRecorder) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// entries returns the lines logged so far whose message contains words.
func (l *logRecorder) entries(t *testing.T, words string) []map[string]any {
	t.Helper()
	var entries []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(l.String()))
	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("a log line is not JSON: %q", scanner.Text())
		}
		if message, _ := entry["message"].(string); strings.Contains(message, words) {
			entries = append(entries, entry)
		}
	}
	return entries
}

// await waits for at least n lines whose message contains words.
func (l *logRecorder) await(t *testing.T, n int, words string) []map[string]any {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if entries := l.entries(t, words); len(entries) >= n {
			return entries
		}
	}
	t.Fatalf("waited 5s for %d lines saying %q; the log is:\n%s", n, words, l.String())
	return nil
}

// rabbitMQWorld is what the runner reads: which organization each project is
// in, and the connections that exist. A test adds to it while the runner runs.
type rabbitMQWorld struct {
	mu            sync.Mutex
	organizations map[uuid.UUID]uuid.UUID
	connections   map[uuid.UUID]entities.ConnectorInstance
}

func newRabbitMQWorld() *rabbitMQWorld {
	return &rabbitMQWorld{
		organizations: map[uuid.UUID]uuid.UUID{},
		connections:   map[uuid.UUID]entities.ConnectorInstance{},
	}
}

func (w *rabbitMQWorld) organizationOf(_ context.Context, project uuid.UUID) (uuid.UUID, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	organization, found := w.organizations[project]
	if !found {
		return uuid.Nil, fmt.Errorf("project %s does not exist", project)
	}
	return organization, nil
}

func (w *rabbitMQWorld) GetConnectorInstance(_ context.Context, id uuid.UUID) (entities.ConnectorInstance, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	connection, found := w.connections[id]
	if !found {
		return entities.ConnectorInstance{}, fmt.Errorf("%w: no such connector instance", apierr.ErrNotFound)
	}
	return connection, nil
}

func (w *rabbitMQWorld) addProject(project, organization uuid.UUID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.organizations[project] = organization
}

func (w *rabbitMQWorld) addConnection(id, project uuid.UUID, connectorKey string, config map[string]any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.connections[id] = entities.ConnectorInstance{
		ID:        id,
		Project:   &entities.Project{ID: project},
		Connector: &entities.Connector{Key: connectorKey},
		Config:    config,
	}
}

// testRabbitMQRunner is a runner over world, logging to logs, that retries
// every few milliseconds rather than every few seconds.
func testRabbitMQRunner(world *rabbitMQWorld, messaging *recordingMessaging, logs *logRecorder) *rabbitMQRunner {
	runner := newRabbitMQRunner(messaging, world, world.organizationOf)
	runner.logger = zerolog.New(logs)
	runner.retryFirst = 10 * time.Millisecond
	runner.retryMost = 40 * time.Millisecond
	return runner
}

func newRecordingMessaging() *recordingMessaging {
	return &recordingMessaging{bridges: make(chan startedBridge, 4), consumers: make(chan startedConsumer, 4)}
}

const testBrokerURL = "amqp://metis:s3cret-broker-password@broker.internal:5672/orders"

func TestARabbitMQBridgeWaitsForItsConnectionAndStartsOnceItExists(t *testing.T) {
	var logs logRecorder
	world, messaging := newRabbitMQWorld(), newRecordingMessaging()
	project, organization, connection := uuid.New(), uuid.New(), uuid.New()
	world.addProject(project, organization)

	runner := testRabbitMQRunner(world, messaging, &logs)
	runner.start(t.Context(), []rabbitMQBridge{{
		target: rabbitMQTarget{project: project, connection: connection},
		topic:  "reverse-charge", exchange: "billing", routingKey: "charges.reverse",
		lock: 7 * time.Minute,
	}}, nil)
	t.Cleanup(func() { runner.stop(context.Background()) })

	// Reported, naming the bridge and what is missing, and tried again with a
	// wait that grows to the ceiling and stays there.
	failures := logs.await(t, 4, "could not start")
	assertNamed(t, failures[0], map[string]string{
		"project": project.String(), "connection": connection.String(), "topic": "reverse-charge",
	})
	if reason, _ := failures[0]["error"].(string); !strings.Contains(reason, "connection "+connection.String()+" does not exist") {
		t.Errorf("the reason given is %q, which does not say the connection is missing", reason)
	}
	var waits []float64
	for _, failure := range failures[:4] {
		wait, _ := failure["retryIn"].(float64)
		waits = append(waits, wait)
	}
	if fmt.Sprint(waits) != "[10 20 40 40]" {
		t.Errorf("waited %v ms between tries, want [10 20 40 40]: doubling, up to the ceiling", waits)
	}
	if len(messaging.bridges) != 0 {
		t.Fatal("a bridge was started before its connection existed")
	}

	world.addConnection(connection, project, serviceimpl.RabbitMQConnectorKey, map[string]any{"url": testBrokerURL})
	bridge := awaitStarted(t, messaging.bridges, "bridge")
	if bridge.url != testBrokerURL || bridge.project != project || bridge.topic != "reverse-charge" || bridge.lock != 7*time.Minute {
		t.Errorf("the bridge was started as %+v", bridge)
	}
	assertActsForOrganization(t, bridge.ctx, organization)

	// The messaging service logs through the context it was given, so its own
	// lines say which connection and broker they are about.
	zerolog.Ctx(bridge.ctx).Info().Msg("a line from the messaging service")
	assertNamed(t, logs.await(t, 1, "a line from the messaging service")[0], map[string]string{
		"connection": connection.String(), "organization": organization.String(), "broker": "broker.internal:5672",
	})
}

// assertNamed checks that a line carries the fields that say which bridge or
// consumer it is about.
func assertNamed(t *testing.T, entry map[string]any, fields map[string]string) {
	t.Helper()
	for field, want := range fields {
		if got := entry[field]; got != want {
			t.Errorf("%s = %v, want %q: %v", field, got, want, entry)
		}
	}
}

func TestARabbitMQConnectionThatCannotBeUsedIsReportedAndNothingStarts(t *testing.T) {
	project, organization, connection := uuid.New(), uuid.New(), uuid.New()
	tests := []struct {
		name   string
		world  func(w *rabbitMQWorld)
		reason string
	}{
		{
			name:   "the project does not exist",
			world:  func(*rabbitMQWorld) {},
			reason: "project " + project.String() + " does not exist",
		},
		{
			name: "the connection is another project's",
			world: func(w *rabbitMQWorld) {
				w.addProject(project, organization)
				w.addConnection(connection, uuid.New(), serviceimpl.RabbitMQConnectorKey, map[string]any{"url": testBrokerURL})
			},
			reason: "belongs to another project",
		},
		{
			name: "the connection is not RabbitMQ",
			world: func(w *rabbitMQWorld) {
				w.addProject(project, organization)
				w.addConnection(connection, project, "slack-message", map[string]any{"webhook_url": "https://hooks.example"})
			},
			reason: "is not a RabbitMQ connection",
		},
		{
			name: "the connection has no URL",
			world: func(w *rabbitMQWorld) {
				w.addProject(project, organization)
				w.addConnection(connection, project, serviceimpl.RabbitMQConnectorKey, map[string]any{"exchange": "orders"})
			},
			reason: "has no RabbitMQ URL",
		},
		{
			name: "the connection's URL does not parse",
			world: func(w *rabbitMQWorld) {
				w.addProject(project, organization)
				w.addConnection(connection, project, serviceimpl.RabbitMQConnectorKey,
					map[string]any{"url": "amqp://metis:s3cret-broker-password@broker.internal:port/"})
			},
			reason: "is not a valid amqp:// or amqps:// URL",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var logs logRecorder
			world, messaging := newRabbitMQWorld(), newRecordingMessaging()
			tc.world(world)
			runner := testRabbitMQRunner(world, messaging, &logs)
			runner.start(t.Context(), nil, []rabbitMQConsumer{{
				target: rabbitMQTarget{project: project, connection: connection},
				queue:  "payments", message: "PaymentReceived",
			}})

			failure := logs.await(t, 1, "A RabbitMQ consumer could not start")[0]
			runner.stop(context.Background())

			if reason, _ := failure["error"].(string); !strings.Contains(reason, tc.reason) {
				t.Errorf("the reason given is %q, want it to say %q", reason, tc.reason)
			}
			assertNamed(t, failure, map[string]string{"project": project.String(), "queue": "payments"})
			if len(messaging.consumers) != 0 {
				t.Error("a consumer was started with a connection that cannot be used")
			}
			if strings.Contains(logs.String(), "s3cret-broker-password") {
				t.Errorf("the broker password is in the log:\n%s", logs.String())
			}
		})
	}
}

// hangingMessaging is a messaging service whose StopAll does not return until
// released: a bridge blocked dialling a broker that has gone away.
type hangingMessaging struct {
	*recordingMessaging
	release chan struct{}
}

func (m *hangingMessaging) StopAll() { <-m.release }

func TestShutdownDoesNotWaitForeverForABrokerThatHasGoneAway(t *testing.T) {
	var logs logRecorder
	world := newRabbitMQWorld()
	messaging := &hangingMessaging{recordingMessaging: newRecordingMessaging(), release: make(chan struct{})}
	t.Cleanup(func() { close(messaging.release) })

	runner := newRabbitMQRunner(messaging, world, world.organizationOf)
	runner.logger = zerolog.New(&logs)
	runner.start(t.Context(), nil, nil)

	budget, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	began := time.Now()
	runner.stop(budget)

	if waited := time.Since(began); waited > 2*time.Second {
		t.Fatalf("shutdown waited %s for a messaging service that never stopped; the budget was 100ms", waited)
	}
	if len(logs.entries(t, "did not stop within the shutdown budget")) != 1 {
		t.Errorf("giving up on them was not reported:\n%s", logs.String())
	}
}

// Stopping ends the retries too, not only what they started: a bridge still
// waiting for its project is not started after the server has begun to stop.
func TestStoppingEndsTheRetries(t *testing.T) {
	var logs logRecorder
	world, messaging := newRabbitMQWorld(), newRecordingMessaging()
	project, connection := uuid.New(), uuid.New()
	runner := testRabbitMQRunner(world, messaging, &logs)
	runner.retryFirst, runner.retryMost = time.Hour, time.Hour
	runner.start(t.Context(), []rabbitMQBridge{{
		target: rabbitMQTarget{project: project, connection: connection},
		topic:  "reverse-charge", exchange: "billing",
	}}, nil)
	logs.await(t, 1, "could not start")

	stopped := make(chan struct{})
	go func() {
		runner.stop(context.Background())
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("stopping waited on a retry that was an hour away")
	}
	if stops := messaging.stops.Load(); stops != 1 {
		t.Fatalf("StopAll ran %d times, want once", stops)
	}
}
