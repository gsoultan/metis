package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
	amqp "github.com/rabbitmq/amqp091-go"
)

// The whole path, against a real broker: a server booted with a bridge and a
// consumer configured publishes an external task of the bridge's topic, and
// correlates a message put on the consumer's queue.
//
// It needs a broker, as the connector's suite does, and skips without one. CI
// sets METIS_TEST_RABBITMQ_URL and fails on any skip.

const rabbitMQURLEnv = "METIS_TEST_RABBITMQ_URL"

func liveBrokerURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv(rabbitMQURLEnv)
	if url == "" {
		t.Skipf("set %s to run this against a live RabbitMQ broker", rabbitMQURLEnv)
	}
	return url
}

// brokerChannel opens a channel of the test's own, closed when the test ends.
func brokerChannel(t *testing.T, url string) *amqp.Channel {
	t.Helper()
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatalf("dial the broker: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("open a channel: %v", err)
	}
	return ch
}

// realRabbitMQApp is an App over a test database with the real facade, and so
// the real messaging service.
func realRabbitMQApp(t *testing.T) *App {
	t.Helper()
	gormDB := testutils.SetupTestDB(t)
	conn := testutils.StormConn(gormDB)
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	facade := services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "rabbitmq-broker-test", nil, nil, nil)
	return &App{db: gormDB, storm: conn, repo: repo, sse: sse, svc: facade}
}

func deployAndStart(t *testing.T, ctx context.Context, a *App, def *entities.ProcessDefinition, vars map[string]any) uuid.UUID {
	t.Helper()
	if _, err := a.svc.CreateDefinition(ctx, def); err != nil {
		t.Fatalf("deploy %s: %v", def.Key, err)
	}
	instance, err := a.svc.StartProcess(ctx, def.Project.ID, def.Key, vars)
	if err != nil {
		t.Fatalf("start %s: %v", def.Key, err)
	}
	return instance
}

// awaitTaskAt waits until the instance has an open task at node.
func awaitTaskAt(t *testing.T, ctx context.Context, a *App, project, instance uuid.UUID, node string) {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		tasks, err := a.svc.ListTasks(ctx, project)
		if err != nil {
			t.Fatalf("list tasks: %v", err)
		}
		for _, task := range tasks {
			if task.Instance != nil && task.Instance.ID == instance && task.NodeID() == node &&
				task.Status == entities.TaskUnclaimed {
				return
			}
		}
	}
	t.Fatalf("30s after the message was queued, instance %s is still not waiting at %q", instance, node)
}

func TestARunningServerBridgesATaskToRabbitMQAndCorrelatesAMessageFromIt(t *testing.T) {
	url := liveBrokerURL(t)
	a := realRabbitMQApp(t)
	seeded := seedRabbitMQConnection(t, a, url)
	ctx := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: seeded.organization.String()})

	// Where the bridge publishes: a queue of the test's own, reached through the
	// default exchange by its name.
	ch := brokerChannel(t, url)
	published, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatalf("declare the queue the bridge publishes to: %v", err)
	}
	deliveries, err := ch.Consume(published.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatalf("consume what the bridge publishes: %v", err)
	}

	// Where the consumer reads: declared durable, as the consumer declares it,
	// so the message can be queued before the consumer has connected.
	payments := "metis-test-payments-" + uuid.NewString()
	if _, err := ch.QueueDeclare(payments, true, false, false, false, nil); err != nil {
		t.Fatalf("declare %s: %v", payments, err)
	}
	t.Cleanup(func() {
		cleanup := brokerChannel(t, url)
		for _, queue := range []string{payments, payments + ".dlq"} {
			if _, err := cleanup.QueueDelete(queue, false, false, false); err != nil {
				t.Errorf("delete %s: %v", queue, err)
			}
		}
	})

	topic := "metis-test-" + uuid.NewString()
	t.Setenv("METIS_RABBITMQ_BRIDGES", fmt.Sprintf(`[{"project":%q,"connection":%q,"topic":%q,"routing_key":%q}]`,
		seeded.project, seeded.connection, topic, published.Name))
	t.Setenv("METIS_RABBITMQ_CONSUMERS", fmt.Sprintf(`[{"project":%q,"connection":%q,"queue":%q,"message":"PaymentReceived"}]`,
		seeded.project, seeded.connection, payments))
	serveOnAnyPort(t)

	charge := deployAndStart(t, ctx, a, externalTaskProcess(seeded.project, topic), nil)
	order := deployAndStart(t, ctx, a, &entities.ProcessDefinition{
		Project: &entities.Project{ID: seeded.project},
		Key:     "awaits-payment",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "await-payment", Type: entities.IntermediateCatchEvent, Properties: map[string]any{
				"message_name": "PaymentReceived", "correlation_key": "${orderId}",
			}},
			{ID: "confirm", Type: entities.UserTask, Name: "Confirm the order"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "await-payment"},
			{ID: "f2", SourceRef: "await-payment", TargetRef: "confirm"},
			{ID: "f3", SourceRef: "confirm", TargetRef: "end"},
		},
	}, map[string]any{"orderId": "order-1"})

	stop, served := runUntilCancelled(t, a)

	// The bridge: the task arrives, as the task, under the bridge's lock.
	select {
	case delivery := <-deliveries:
		var task struct {
			ID              string `json:"id"`
			Topic           string `json:"topic"`
			WorkerID        string `json:"worker_id"`
			ProcessInstance struct {
				ID string `json:"id"`
			} `json:"process_instance"`
		}
		if err := json.Unmarshal(delivery.Body, &task); err != nil {
			t.Fatalf("the bridge published something that is not a task: %v: %s", err, delivery.Body)
		}
		if task.Topic != topic || task.ProcessInstance.ID != charge.String() || task.WorkerID != "messaging-bridge" {
			t.Errorf("the bridge published %s", delivery.Body)
		}
		if delivery.Headers["task_id"] != task.ID {
			t.Errorf("the task_id header is %v, want %s", delivery.Headers["task_id"], task.ID)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("30s into a running server, the bridge has published nothing")
	}

	// The consumer: a message queued for order-1 moves its instance on.
	err = ch.PublishWithContext(t.Context(), "", payments, false, false, amqp.Publishing{
		ContentType: "application/json",
		Body:        []byte(`{"correlation_key":"order-1","amount":4200}`),
	})
	if err != nil {
		t.Fatalf("queue the payment: %v", err)
	}
	awaitTaskAt(t, ctx, a, seeded.project, order, "confirm")

	stop()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("the server did not shut down cleanly: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the server did not shut down within 30s")
	}
}
