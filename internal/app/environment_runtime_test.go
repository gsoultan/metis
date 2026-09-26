package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/gorms"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

// Deleting an environment removed its row and nothing else. Its port kept
// answering, its workers kept polling its database and its connections stayed
// open until the next restart, although the service said that removing the
// row stops the runtime being served. An administrator who removed a runtime
// to shut people out of it had not. Disabling one did no more.
func TestADeletedOrDisabledEnvironmentStopsBeingServed(t *testing.T) {
	watchEvery, closeAfter := environmentWatchEvery, environmentCloseAfter
	environmentWatchEvery, environmentCloseAfter = 50*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { environmentWatchEvery, environmentCloseAfter = watchEvery, closeAfter })

	for name, remove := range map[string]func(ctx context.Context, repo repositories.Repository, env models.EnvironmentModel) error{
		"deleted": func(ctx context.Context, repo repositories.Repository, env models.EnvironmentModel) error {
			return repo.Environment().Delete(ctx, uuid.UUID(env.ID))
		},
		"disabled": func(ctx context.Context, repo repositories.Repository, env models.EnvironmentModel) error {
			env.Enabled = false
			return repo.Environment().Update(ctx, env)
		},
	} {
		t.Run(name, func(t *testing.T) { serveThenRemove(t, name, remove) })
	}
}

func serveThenRemove(t *testing.T, how string,
	remove func(ctx context.Context, repo repositories.Repository, env models.EnvironmentModel) error) {
	gormDB := testutils.SetupTestDB(t)
	conn := testutils.StormConn(gormDB)
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	a := &App{db: gormDB, storm: conn, repo: repo, sse: sse,
		svc: services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "environment-stop-test", nil, nil, nil, func(*gorm.DB) {})}
	ctx := entities.WithSystemContext(t.Context())

	// The environment's own connections, to the test's schema. Stopping it
	// closes them, and closing the test's would end the test.
	var schema string
	if err := gormDB.WithContext(ctx).Raw("SHOW search_path").Scan(&schema).Error; err != nil {
		t.Fatalf("read the test schema: %v", err)
	}
	envDB, err := gorms.Open("postgres", envvar.Get(testutils.PostgresDSNEnv)+" search_path="+schema)
	if err != nil {
		t.Fatalf("open the environment's connection: %v", err)
	}
	envPool, err := pgxpool.NewWithConfig(ctx, conn.Main().Config())
	if err != nil {
		t.Fatalf("open the environment's pool: %v", err)
	}

	port := freePort(t)
	id := uuid.New()
	env := models.EnvironmentModel{
		Base: models.Base{ID: models.UUID(id)}, ProjectID: models.UUID(uuid.New()),
		Name: "staging", Port: port, Driver: "postgres", Enabled: true,
	}
	if err := repo.Environment().Create(ctx, env); err != nil {
		t.Fatalf("record the environment: %v", err)
	}
	gorms.RegisterEnvironmentDB(id, envDB)
	conn.RegisterEnvironment(id, envPool)
	t.Cleanup(func() {
		if db, open := gorms.ForgetEnvironmentDB(id); open {
			closeDB(db)
		}
		if pool, open := conn.ForgetEnvironment(id); open {
			pool.Close()
		}
	})

	runCtx, stopRunning := context.WithCancel(t.Context())
	g, groupCtx := errgroup.WithContext(runCtx)
	t.Cleanup(func() {
		stopRunning()
		_ = g.Wait()
	})
	a.startBackgroundWork(runCtx)
	a.serveEnvironments(groupCtx, g, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	address := fmt.Sprintf("127.0.0.1:%d", port)
	eventually(t, "the environment was never served", func() bool {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/", nil)
		if err != nil {
			return false
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusNoContent
	})

	if err := remove(ctx, repo, env); err != nil {
		t.Fatalf("remove the environment: %v", err)
	}

	eventually(t, "five seconds after the environment was "+how+", its port still answers", func() bool {
		var dialer net.Dialer
		dialCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		c, err := dialer.DialContext(dialCtx, "tcp", address)
		if err != nil {
			return true
		}
		_ = c.Close()
		return false
	})
	eventually(t, "five seconds after the environment was "+how+", its database connections are still open", func() bool {
		_, gormOpen := gorms.EnvironmentDB(id)
		_, stormOpen := conn.EnvironmentPools()[id]
		return !gormOpen && !stormOpen
	})
}

// eventually waits up to five seconds for done, and fails with why if it never is.
func eventually(t *testing.T, why string, done func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if done() {
			return
		}
	}
	t.Fatal(why)
}

// freePort finds a port nothing is listening on.
func freePort(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	l, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		t.Fatalf("free the port: %v", err)
	}
	return port
}
