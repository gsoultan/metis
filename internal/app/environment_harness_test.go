package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/gorms"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// environmentHarness is a server as far as its environments go: the main
// store, a project for environments to belong to, and databases of their own
// for them to be opened on the way the server opens them.
type environmentHarness struct {
	t       *testing.T
	app     *App
	conn    *db.Conn
	project uuid.UUID
	// ctx is a system context, which is how the watcher reads the registry and
	// how an administrator's change reaches it in these tests.
	ctx     context.Context
	created []uuid.UUID
}

func newEnvironmentHarness(t *testing.T) *environmentHarness {
	t.Helper()
	gormDB, conn := testutils.SetupTestStore(t)
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	a := &App{db: gormDB, storm: conn, repo: repo, sse: sse,
		svc: services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "environment-test", nil, nil, nil, func(*gorm.DB) {})}

	ctx := entities.WithSystemContext(t.Context())
	org, err := a.svc.CreateOrganization(ctx, "Org", "")
	if err != nil {
		t.Fatalf("create the organization: %v", err)
	}
	tenantCtx := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})
	project, err := a.svc.CreateProject(tenantCtx, org.ID, "Purchase Approval", "")
	if err != nil {
		t.Fatalf("create the project: %v", err)
	}
	return &environmentHarness{t: t, app: a, conn: conn, project: project.ID, ctx: ctx}
}

// serve starts serving the environments, the way a server does at boot.
func (h *environmentHarness) serve(handler http.Handler) {
	h.t.Helper()
	ctx, cancel := context.WithCancel(h.t.Context())
	h.app.serveEnvironments(ctx, handler)
	h.t.Cleanup(func() {
		cancel()
		h.app.environments.work.Wait()
		h.closeEnvironments()
	})
}

// closeEnvironments lets go of every connection the test's environments hold,
// so their databases can be dropped.
func (h *environmentHarness) closeEnvironments() {
	for _, id := range h.created {
		if gormDB, open := gorms.ForgetEnvironmentDB(id); open {
			closeDB(gormDB)
		}
		if pool, open := h.conn.ForgetEnvironment(id); open {
			pool.Close()
		}
	}
}

// environment records an enabled environment of the harness's project, on a
// free port, whose database is the one named.
func (h *environmentHarness) environment(name, database string) models.EnvironmentModel {
	h.t.Helper()
	env := models.EnvironmentModel{
		Base:      models.Base{ID: models.UUID(uuid.Must(uuid.NewV7()))},
		ProjectID: models.UUID(h.project),
		Name:      name,
		Port:      freePort(h.t),
		Driver:    "postgres",
		Enabled:   true,
	}
	env.Connection = connectionTo(h.t, database)
	if err := h.app.repo.Environment().Create(h.ctx, env); err != nil {
		h.t.Fatalf("record the %s environment: %v", name, err)
	}
	h.created = append(h.created, uuid.UUID(env.ID))
	return env
}

// save stores an administrator's change to an environment.
func (h *environmentHarness) save(env models.EnvironmentModel) {
	h.t.Helper()
	if err := h.app.repo.Environment().Update(h.ctx, env); err != nil {
		h.t.Fatalf("save the %s environment: %v", env.Name, err)
	}
}

// connectionTo is the connection an administrator would enter for a database
// on the test's PostgreSQL server.
func connectionTo(t *testing.T, database string) models.EncryptedMap {
	t.Helper()
	fields := dsnFields(t)
	port, err := strconv.Atoi(fields["port"])
	if err != nil {
		port = 5432
	}
	return models.EncryptedMap{
		"host":        fields["host"],
		"port":        port,
		"username":    fields["user"],
		"password":    fields["password"],
		"db_name":     database,
		"ssl_enabled": fields["sslmode"] != "" && fields["sslmode"] != "disable",
	}
}

func dsnFields(t *testing.T) map[string]string {
	t.Helper()
	dsn := envvar.Get(testutils.PostgresDSNEnv)
	if dsn == "" {
		t.Skipf("set %s to run this against a live PostgreSQL instance", testutils.PostgresDSNEnv)
	}
	fields := map[string]string{}
	for _, pair := range strings.Fields(dsn) {
		if key, value, ok := strings.Cut(pair, "="); ok {
			fields[key] = value
		}
	}
	return fields
}

// unusedDatabaseName names a database nothing has created.
func unusedDatabaseName() string {
	return "metis_env_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
}

// scratchDatabase creates a database for one environment, dropped when the
// test ends.
//
// A database rather than the schema the rest of the suite gets: an
// environment's connection names a database and nothing narrower, and what is
// under test is opening it the way the server does.
func scratchDatabase(t *testing.T) string {
	t.Helper()
	name := unusedDatabaseName()
	createDatabase(t, name)
	return name
}

func createDatabase(t *testing.T, name string) {
	t.Helper()
	admin, err := gorms.Open("postgres", envvar.Get(testutils.PostgresDSNEnv))
	if err != nil {
		t.Fatalf("open the maintenance connection: %v", err)
	}
	t.Cleanup(func() { closeDB(admin) })
	if err := admin.Exec("CREATE DATABASE " + name).Error; err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	t.Cleanup(func() {
		// FORCE, because a pool that has not finished closing still holds
		// connections to it — and a database left behind on a shared server is
		// somebody else's problem tomorrow.
		if err := admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)").Error; err != nil {
			t.Logf("could not drop %s: %v", name, err)
		}
	})
}

// behindTheServer opens one of the test's databases directly, the way
// somebody looking behind the server's back would.
func behindTheServer(t *testing.T, database string) *gorm.DB {
	t.Helper()
	fields := dsnFields(t)
	fields["dbname"] = database
	pairs := make([]string, 0, len(fields))
	for key, value := range fields {
		pairs = append(pairs, key+"="+value)
	}
	conn, err := gorms.Open("postgres", strings.Join(pairs, " "))
	if err != nil {
		t.Fatalf("open %s: %v", database, err)
	}
	t.Cleanup(func() { closeDB(conn) })
	return conn
}

// noContent is a server that answers everything with 204, so a probe can tell
// the environment's listener from anything else on the port.
var noContent = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
})

// answers reports whether the port serves the test's handler.
func answers(t *testing.T, port int, path string) bool {
	t.Helper()
	client := http.Client{Timeout: time.Second}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d%s", port, path), nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusNoContent
}

// refuses reports whether nothing accepts connections on the port.
func refuses(t *testing.T, port int) bool {
	t.Helper()
	dialer := net.Dialer{Timeout: 100 * time.Millisecond}
	c, err := dialer.DialContext(t.Context(), "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return true
	}
	_ = c.Close()
	return false
}

// holdPort keeps a port taken, on every interface, until the returned
// function is called or the test ends.
func holdPort(t *testing.T, port int) func() {
	t.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Fatalf("take port %d: %v", port, err)
	}
	release := func() { _ = l.Close() }
	t.Cleanup(release)
	return release
}

// fastEnvironmentChecks makes the watcher check every 50ms and close a
// stopped environment's connections after 100ms, for the rest of the test.
//
// Registered before anything that starts the watcher, so it is undone after
// the watcher has stopped: cleanups run last-registered first.
func fastEnvironmentChecks(t *testing.T) {
	t.Helper()
	watchEvery, closeAfter := environmentWatchEvery, environmentCloseAfter
	environmentWatchEvery, environmentCloseAfter = 50*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { environmentWatchEvery, environmentCloseAfter = watchEvery, closeAfter })
}

// within waits up to limit for done, and fails with why if it never is.
//
// Longer than eventually's five seconds: starting an environment migrates a
// fresh database, which takes seconds of its own on a loaded machine.
func within(t *testing.T, limit time.Duration, why string, done func() bool) {
	t.Helper()
	for deadline := time.Now().Add(limit); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if done() {
			return
		}
	}
	t.Fatal(why)
}

// startLimit is how long an environment may take to be served once it can be.
const startLimit = 30 * time.Second
