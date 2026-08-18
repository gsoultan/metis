// Package outage simulates infrastructure failure under a running engine and
// asserts two properties an orchestrator must hold:
//
//  1. During an outage the system fails fast and tells the truth — operations
//     return errors within a bound instead of hanging, and the readiness probe
//     answers 503 so a load balancer stops sending traffic.
//  2. After the outage nothing is lost — work created before the failure is
//     still there, still completable, and the instance finishes.
//
// The database is severed at the TCP layer through a proxy the test controls,
// which is the shape real outages take: connections die mid-flight and new
// ones are refused. Broker outages are covered at the unit level in
// server/domains/services/impl/messaging_test.go (the consumer reconnect
// loop); the network dimension is this same proxy.
//
// Requires GOBPM_TEST_POSTGRES_DSN, like every test that needs a real engine
// underneath — see AGENTS.md §4.
package outage

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/internal/pkg/health"
	"github.com/gsoultan/gobpm/server/domains/entities"
	handlersimpl "github.com/gsoultan/gobpm/server/domains/handlers/impl"
	observersimpl "github.com/gsoultan/gobpm/server/domains/observers/impl"
	serviceimpl "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// severableProxy is a TCP proxy the test can cut and restore. Severing closes
// the listener and every live connection, so clients see exactly what a dead
// database looks like: resets on existing connections, refusals on new ones.
type severableProxy struct {
	t      *testing.T
	target string

	mu       sync.Mutex
	addr     string
	listener net.Listener
	conns    map[net.Conn]struct{}
}

func newSeverableProxy(t *testing.T, target string) *severableProxy {
	t.Helper()
	p := &severableProxy{t: t, target: target, conns: make(map[net.Conn]struct{})}

	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}
	p.addr = listener.Addr().String()
	p.listener = listener
	go p.acceptLoop(listener)

	t.Cleanup(p.Sever)
	return p
}

func (p *severableProxy) Addr() string { return p.addr }

func (p *severableProxy) acceptLoop(listener net.Listener) {
	for {
		client, err := listener.Accept()
		if err != nil {
			return // severed
		}
		upstream, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(context.Background(), "tcp", p.target)
		if err != nil {
			_ = client.Close()
			continue
		}

		p.mu.Lock()
		p.conns[client] = struct{}{}
		p.conns[upstream] = struct{}{}
		p.mu.Unlock()

		pipe := func(dst, src net.Conn) {
			_, _ = io.Copy(dst, src)
			_ = dst.Close()
			_ = src.Close()
		}
		go pipe(upstream, client)
		go pipe(client, upstream)
	}
}

// Sever cuts the database off: no new connections, all existing ones killed.
func (p *severableProxy) Sever() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.listener != nil {
		_ = p.listener.Close()
		p.listener = nil
	}
	for conn := range p.conns {
		_ = conn.Close()
	}
	p.conns = make(map[net.Conn]struct{})
}

// Restore brings the database back on the same address, as a recovered
// database would come back on the same host and port.
func (p *severableProxy) Restore() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.listener != nil {
		return
	}
	listener, err := new(net.ListenConfig).Listen(p.t.Context(), "tcp", p.addr)
	if err != nil {
		p.t.Fatalf("proxy restore on %s: %v", p.addr, err)
	}
	p.listener = listener
	go p.acceptLoop(listener)
}

// proxiedDSN rewrites the env DSN's host and port to go through the proxy,
// and returns the proxy. Skips the test when no DSN is configured.
func proxiedDSN(t *testing.T) (string, *severableProxy) {
	t.Helper()
	dsn := os.Getenv(testutils.PostgresDSNEnv)
	if dsn == "" {
		t.Skipf("set %s to run outage simulations against a live PostgreSQL", testutils.PostgresDSNEnv)
	}

	host, port := "localhost", "5432"
	var rest []string
	for _, field := range strings.Fields(dsn) {
		switch {
		case strings.HasPrefix(field, "host="):
			host = strings.TrimPrefix(field, "host=")
		case strings.HasPrefix(field, "port="):
			port = strings.TrimPrefix(field, "port=")
		default:
			rest = append(rest, field)
		}
	}

	proxy := newSeverableProxy(t, net.JoinHostPort(host, port))
	proxyHost, proxyPort, err := net.SplitHostPort(proxy.Addr())
	if err != nil {
		t.Fatalf("split proxy addr: %v", err)
	}
	rewritten := strings.Join(append(rest, "host="+proxyHost, "port="+proxyPort), " ")
	return rewritten, proxy
}

// TestReadinessTellsTheTruthThroughAnOutage exercises the exact probe
// semantics production wires up: the checker is the same PingContext the app
// registers, behind the same health.Wrap.
//
// The property matters operationally: a readiness probe that keeps answering
// 200 through a database outage keeps the replica in the load balancer,
// converting one dependency failure into user-visible errors on every request.
func TestReadinessTellsTheTruthThroughAnOutage(t *testing.T) {
	dsn, proxy := proxiedDSN(t)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open through proxy: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	// Mirrors App.readinessCheckers — if that shape changes, change both.
	handler := health.Wrap(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}),
		map[string]health.Checker{
			"database": health.CheckerFunc(func(ctx context.Context) error {
				return sqlDB.PingContext(ctx)
			}),
		},
	)

	probe := func() int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, health.ReadinessPath, nil))
		return rec.Code
	}

	if code := probe(); code != http.StatusOK {
		t.Fatalf("healthy database: /readyz = %d, want 200", code)
	}

	proxy.Sever()
	start := time.Now()
	if code := probe(); code != http.StatusServiceUnavailable {
		t.Fatalf("severed database: /readyz = %d, want 503", code)
	}
	// The probe must fail fast, not hang: a probe that times out reads as "no
	// answer", which orchestrators treat as "still starting" for far longer.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("the failing probe took %v; it must answer, not hang", elapsed)
	}

	proxy.Restore()
	deadline := time.Now().Add(15 * time.Second)
	for probe() != http.StatusOK {
		if time.Now().After(deadline) {
			t.Fatal("the database is back but /readyz never recovered")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// newEngine builds the same service stack the backfill tests use, over an
// arbitrary database. Duplicated from tests/postgres deliberately: test
// packages cannot import each other, and a shared harness in testutils would
// couple every suite to the engine's constructor churn.
func newEngine(t *testing.T, db *gorm.DB) (repositories.Repository, *serviceimpl.Engine, uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	repo := repositories.NewRepository(db)
	dispatcher := observersimpl.NewEventDispatcher()
	engine := serviceimpl.NewExecutionEngine(repo, dispatcher)
	connectorSvc := serviceimpl.NewConnectorService(repo)
	taskSvc := serviceimpl.NewTaskService(repo, engine, serviceimpl.NewAuditWriter(repo.Audit()))
	jobSvc := serviceimpl.NewJobService(repo, engine, connectorSvc, serviceimpl.NewNoOpLocker(), handlersimpl.NewErrorBoundaryMatcher())
	externalTaskSvc := serviceimpl.NewExternalTaskService(repo, engine)
	decisionSvc := serviceimpl.NewDecisionService(repo, serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator()))

	engine.Apply(
		serviceimpl.WithHandlerFactory(handlersimpl.NewNodeHandlerFactory(
			engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc, repo.Subscription(), serviceimpl.NewAuditWriter(repo.Audit()))),
		serviceimpl.WithJobService(jobSvc),
	)

	orgSvc := serviceimpl.NewOrganizationService(repo)
	projectSvc := serviceimpl.NewProjectService(repo)
	org, err := orgSvc.CreateOrganization(ctx, "Outage Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	proj, err := projectSvc.CreateProject(ctx, org.ID, "Outage Project", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return repo, engine, proj.ID
}

// externalDefinition is start → external service task → end: the smallest
// process that leaves durable work parked mid-flight.
func externalDefinition(projectID uuid.UUID, key string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     key,
		Name:    "Outage drill",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: "work", Type: entities.ServiceTask, ExternalTopic: "outage-topic", Incoming: []string{"f1"}, Outgoing: []string{"f2"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "work"},
			{ID: "f2", SourceRef: "work", TargetRef: "end"},
		},
	}
}

// TestEngineSurvivesADatabaseOutage is the recovery drill from
// docs/recovery.md §4, automated:
//
//	work exists → the database dies → operations fail fast, loudly →
//	the database returns → the same work is still there, completes, and
//	the instance finishes.
func TestEngineSurvivesADatabaseOutage(t *testing.T) {
	dsn, proxy := proxiedDSN(t)
	t.Setenv(testutils.PostgresDSNEnv, dsn)
	db := testutils.SetupPostgresDB(t, 4)

	ctx := t.Context()
	repo, engine, projectID := newEngine(t, db)
	defSvc := serviceimpl.NewDefinitionService(repo)

	if _, err := defSvc.CreateDefinition(ctx, externalDefinition(projectID, "outage-drill")); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	instanceID, err := engine.StartProcess(ctx, projectID, "outage-drill", map[string]any{"amount": 7})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	externalSvc := serviceimpl.NewExternalTaskService(repo, engine)
	tasks, err := externalSvc.FetchAndLock(ctx, "outage-topic", "drill-worker", 1, 60_000)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("before the outage: tasks=%d err=%v, want exactly the parked work", len(tasks), err)
	}
	parked := tasks[0]

	// ---- the outage ----
	proxy.Sever()

	start := time.Now()
	if _, err := engine.StartProcess(ctx, projectID, "outage-drill", nil); err == nil {
		t.Fatal("starting a process during the outage reported success; that instance does not exist")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("the failing start took %v; an outage must fail fast, not hang callers", elapsed)
	}
	if _, err := externalSvc.FetchAndLock(ctx, "outage-topic", "drill-worker", 1, 60_000); err == nil {
		t.Fatal("fetch-and-lock during the outage reported success")
	}

	// ---- recovery ----
	proxy.Restore()

	// The pool has to notice its dead connections and redial; give it a bounded
	// window rather than asserting on the first attempt.
	var recovered bool
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := engine.GetInstance(ctx, instanceID); err == nil {
			recovered = true
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !recovered {
		t.Fatal("the database is back but the engine never recovered")
	}

	// The parked work survived and completes. The lock from before the outage
	// is still ours (same worker ID), so completion is legitimate.
	if err := externalSvc.Complete(ctx, parked.ID, "drill-worker", map[string]any{"done": true}); err != nil {
		t.Fatalf("completing the parked task after recovery: %v", err)
	}

	inst, err := engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("get instance after completion: %v", err)
	}
	if string(inst.Status) != "completed" {
		t.Fatalf("instance is %q after the drill, want completed — the outage cost durable work", inst.Status)
	}

	// And the world keeps turning: new work starts cleanly after recovery.
	if _, err := engine.StartProcess(ctx, projectID, "outage-drill", nil); err != nil {
		t.Fatalf("starting fresh work after recovery: %v", err)
	}
}
