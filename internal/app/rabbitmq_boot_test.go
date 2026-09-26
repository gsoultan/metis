package app

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// The README advertised RabbitMQ inbound correlation and an external-task
// bridge, and nothing started either: StartBridge, StartInboundConsumer and
// StopAll had no callers, so a running server had no broker connection at all.
// These boot a server the way Run does — background work, then the listeners —
// with the messaging service replaced by one that records what it is asked to
// do, so no broker is needed to see what the server starts and stops.

// recordingMessaging is the real facade with its messaging service replaced.
type recordingMessaging struct {
	services.ServiceFacade
	bridges   chan startedBridge
	consumers chan startedConsumer
	stops     atomic.Int32
}

type startedBridge struct {
	ctx        context.Context
	project    uuid.UUID
	topic      string
	url        string
	exchange   string
	routingKey string
	lock       time.Duration
}

type startedConsumer struct {
	ctx     context.Context
	project uuid.UUID
	url     string
	queue   string
	message string
}

func (m *recordingMessaging) StartBridge(ctx context.Context, project uuid.UUID, topic, url, exchange, routingKey string, lock time.Duration) error {
	m.bridges <- startedBridge{ctx: ctx, project: project, topic: topic, url: url, exchange: exchange, routingKey: routingKey, lock: lock}
	return nil
}

func (m *recordingMessaging) StartInboundConsumer(ctx context.Context, project uuid.UUID, url, queue, message string) error {
	m.consumers <- startedConsumer{ctx: ctx, project: project, url: url, queue: queue, message: message}
	return nil
}

func (m *recordingMessaging) StopAll() { m.stops.Add(1) }

// rabbitMQTestApp is an App over a test database, with the facade wrapped.
func rabbitMQTestApp(t *testing.T) (*App, *recordingMessaging) {
	t.Helper()
	gormDB := testutils.SetupTestDB(t)
	conn := testutils.StormConn(gormDB)
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	facade := services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "rabbitmq-boot-test", nil, nil, nil)
	recorder := &recordingMessaging{
		ServiceFacade: facade,
		bridges:       make(chan startedBridge, 4),
		consumers:     make(chan startedConsumer, 4),
	}
	return &App{db: gormDB, storm: conn, repo: repo, sse: sse, svc: recorder}, recorder
}

// seededRabbitMQConnection is a project with a RabbitMQ connection configured
// on it, as an administrator would on the Connectors page.
type seededRabbitMQConnection struct {
	organization uuid.UUID
	project      uuid.UUID
	connection   uuid.UUID
	url          string
}

func seedRabbitMQConnection(t *testing.T, a *App, url string) seededRabbitMQConnection {
	t.Helper()
	ctx, organization, project := testutils.ScopedProject(t, a.repo)

	catalogue, err := a.svc.ListConnectors(ctx)
	if err != nil {
		t.Fatalf("read the connector catalogue: %v", err)
	}
	var rabbitMQ *entities.Connector
	for i := range catalogue {
		if catalogue[i].Key == "rabbitmq-publish" {
			rabbitMQ = &catalogue[i]
		}
	}
	if rabbitMQ == nil {
		t.Fatal("the catalogue has no RabbitMQ connector")
	}

	connection, err := a.svc.CreateConnectorInstance(ctx, entities.ConnectorInstance{
		Project:   &entities.Project{ID: project},
		Connector: &entities.Connector{ID: rabbitMQ.ID},
		Name:      "Orders broker",
		Config:    map[string]any{"url": url, "exchange": "orders"},
	})
	if err != nil {
		t.Fatalf("configure the connection: %v", err)
	}
	return seededRabbitMQConnection{organization: organization, project: project, connection: connection.ID, url: url}
}

// serveOnAnyPort keeps the listeners runServers opens off every port a real
// installation, or another test, might hold.
func serveOnAnyPort(t *testing.T) {
	t.Helper()
	t.Setenv("METIS_HTTP_ADDRESS", "127.0.0.1:0")
	t.Setenv("METIS_METRICS_ENABLED", "false")
	t.Setenv("METIS_PPROF_ENABLED", "false")
	t.Setenv("METIS_GRPC_ADDRESS", "")
}

// runUntilCancelled boots a the way Run does after setup, and returns what
// ends the server and what runServers returned once it has.
func runUntilCancelled(t *testing.T, a *App) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	a.startBackgroundWork(ctx)
	served := make(chan error, 1)
	go func() { served <- a.runServers(ctx) }()
	return cancel, served
}

func awaitStarted[T any](t *testing.T, started <-chan T, what string) T {
	t.Helper()
	select {
	case one := <-started:
		return one
	case <-time.After(15 * time.Second):
		t.Fatalf("the server has been up for 15s and no %s was started", what)
	}
	var none T
	return none
}

// assertActsForOrganization checks the context a bridge or consumer was given:
// scoped to the project's organization, so its reads see that tenant's rows,
// and not marked as system work, which would see every tenant's.
func assertActsForOrganization(t *testing.T, ctx context.Context, organization uuid.UUID) {
	t.Helper()
	tenant, ok := entities.TenantContextFrom(ctx)
	if !ok || tenant.TenantID != organization.String() {
		t.Errorf("it was started for tenant %q, want the project's organization %s", tenant.TenantID, organization)
	}
	if entities.IsSystemContext(ctx) {
		t.Error("it was started as system work, which reads every organization's rows")
	}
}

func TestTheRabbitMQBridgesAndConsumersAnOperatorNamesStartWithTheServerAndStopWithIt(t *testing.T) {
	a, recorder := rabbitMQTestApp(t)
	seeded := seedRabbitMQConnection(t, a, "amqp://metis:s3cret-broker-password@broker.internal:5672/orders")
	t.Setenv("METIS_RABBITMQ_BRIDGES", fmt.Sprintf(
		`[{"project":%q,"connection":%q,"topic":"reverse-charge","exchange":"billing","routing_key":"charges.reverse"}]`,
		seeded.project, seeded.connection))
	t.Setenv("METIS_RABBITMQ_CONSUMERS", fmt.Sprintf(
		`[{"project":%q,"connection":%q,"queue":"payments","message":"PaymentReceived"}]`,
		seeded.project, seeded.connection))
	serveOnAnyPort(t)

	stop, served := runUntilCancelled(t, a)

	bridge := awaitStarted(t, recorder.bridges, "bridge")
	if bridge.project != seeded.project || bridge.topic != "reverse-charge" ||
		bridge.exchange != "billing" || bridge.routingKey != "charges.reverse" {
		t.Errorf("the bridge was started as %+v, not as configured", bridge)
	}
	// The connection's own URL, read through the service: the API masks it,
	// and a bridge handed the mask would dial nothing.
	if bridge.url != seeded.url {
		t.Errorf("the bridge was given the URL %q, want the connection's", bridge.url)
	}
	assertActsForOrganization(t, bridge.ctx, seeded.organization)

	consumer := awaitStarted(t, recorder.consumers, "consumer")
	if consumer.project != seeded.project || consumer.queue != "payments" ||
		consumer.message != "PaymentReceived" || consumer.url != seeded.url {
		t.Errorf("the consumer was started as %+v, not as configured", consumer)
	}
	assertActsForOrganization(t, consumer.ctx, seeded.organization)

	stop()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("the server did not shut down cleanly: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the server did not shut down within 30s")
	}
	if stops := recorder.stops.Load(); stops != 1 {
		t.Fatalf("shutting the server down stopped the bridges and consumers %d times, want once", stops)
	}
}

// A project or a connection that does not exist is somebody's mistake, and it
// must not cost the installation its server.
func TestABridgeForAProjectThatDoesNotExistDoesNotStopTheServer(t *testing.T) {
	a, recorder := rabbitMQTestApp(t)
	t.Setenv("METIS_RABBITMQ_BRIDGES", fmt.Sprintf(
		`[{"project":%q,"connection":%q,"topic":"reverse-charge","exchange":"billing","routing_key":"charges.reverse"}]`,
		uuid.New(), uuid.New()))
	serveOnAnyPort(t)

	stop, served := runUntilCancelled(t, a)

	select {
	case bridge := <-recorder.bridges:
		t.Fatalf("a bridge was started for a project that does not exist: %+v", bridge)
	case err := <-served:
		t.Fatalf("the server stopped over a bridge it could not start: %v", err)
	case <-time.After(2 * time.Second):
	}

	stop()
	if err := <-served; err != nil {
		t.Fatalf("the server did not shut down cleanly: %v", err)
	}
}
