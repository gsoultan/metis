package app

import (
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/gorms"
	"github.com/gsoultan/metis/server/repositories/models"
)

// Deleting an environment removed its row and nothing else. Its port kept
// answering, its workers kept polling its database and its connections stayed
// open until the next restart, although the service said that removing the
// row stops the runtime being served. An administrator who removed a runtime
// to shut people out of it had not. Disabling one did no more.
func TestADeletedOrDisabledEnvironmentStopsBeingServed(t *testing.T) {
	for name, remove := range map[string]func(h *environmentHarness, env models.EnvironmentModel){
		"deleted": func(h *environmentHarness, env models.EnvironmentModel) {
			if err := h.app.repo.Environment().Delete(h.ctx, uuid.UUID(env.ID)); err != nil {
				h.t.Fatalf("delete the environment: %v", err)
			}
		},
		"disabled": func(h *environmentHarness, env models.EnvironmentModel) {
			env.Enabled = false
			h.save(env)
		},
	} {
		t.Run(name, func(t *testing.T) { serveThenRemove(t, name, remove) })
	}
}

func serveThenRemove(t *testing.T, how string, remove func(h *environmentHarness, env models.EnvironmentModel)) {
	fastEnvironmentChecks(t)
	h := newEnvironmentHarness(t)
	staging := h.environment("staging", scratchDatabase(t))
	h.serve(noContent)
	within(t, startLimit, "the environment was never served", func() bool { return answers(t, staging.Port, "/") })

	remove(h, staging)

	eventually(t, "five seconds after the environment was "+how+", its port still answers", func() bool {
		return refuses(t, staging.Port)
	})
	id := uuid.UUID(staging.ID)
	eventually(t, "five seconds after the environment was "+how+", its database connections are still open", func() bool {
		_, gormOpen := gorms.EnvironmentDB(id)
		_, stormOpen := h.conn.EnvironmentPools()[id]
		return !gormOpen && !stormOpen
	})
}

// eventually waits up to five seconds for done, and fails with why if it never is.
func eventually(t *testing.T, why string, done func() bool) {
	t.Helper()
	within(t, 5*time.Second, why, done)
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
