package app

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/gorms"
	"github.com/rs/zerolog/log"
)

// environmentWatchEvery is how soon every replica stops serving an environment
// after it is deleted or disabled. A var so a test need not wait for it.
var environmentWatchEvery = 15 * time.Second

// environmentCloseAfter is how long a stopped environment's connections stay
// open: the listener's shutdown time, so a request that was in flight when the
// environment went can finish.
var environmentCloseAfter = httpShutdownTimeout

// environmentRuntimes is what this replica runs for each environment: the
// listener on its port and its background workers. Each part runs under a
// context this cancels, so an environment can be stopped without a restart.
//
// Deleting an environment used to remove its row and nothing else. Its port
// kept answering, its workers kept polling its database and its connections
// stayed open until the next restart, although the service said removing the
// row stops the runtime being served.
type environmentRuntimes struct {
	mu    sync.Mutex
	stops map[uuid.UUID][]context.CancelFunc
}

// run derives the context one part of an environment's runtime runs under.
func (r *environmentRuntimes) run(ctx context.Context, id uuid.UUID) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stops == nil {
		r.stops = map[uuid.UUID][]context.CancelFunc{}
	}
	r.stops[id] = append(r.stops[id], cancel)
	return ctx
}

// running returns the environments this replica runs anything for.
func (r *environmentRuntimes) running() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]uuid.UUID, 0, len(r.stops))
	for id := range r.stops {
		ids = append(ids, id)
	}
	return ids
}

// stop cancels everything one environment runs, and reports whether there was
// anything to cancel.
func (r *environmentRuntimes) stop(id uuid.UUID) bool {
	r.mu.Lock()
	cancels := r.stops[id]
	delete(r.stops, id)
	r.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	return len(cancels) > 0
}

// watchEnvironments stops, on every replica, the environments that were
// deleted or disabled since they were opened at boot.
//
// Only stopping. An environment created, re-enabled or re-pointed at another
// database is served from the next restart, as before: opening one means
// migrating its database and binding a port, which boot does with somebody
// watching.
func (a *App) watchEnvironments(ctx context.Context) {
	ctx = entities.WithSystemContext(ctx)
	go func() {
		ticker := time.NewTicker(environmentWatchEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.stopRemovedEnvironments(ctx)
			}
		}
	}()
}

// stopRemovedEnvironments stops each environment this replica runs that is no
// longer there, or no longer enabled.
func (a *App) stopRemovedEnvironments(ctx context.Context) {
	running := a.environments.running()
	if len(running) == 0 {
		return
	}
	rows, err := a.repo.Environment().ListAll(ctx)
	if err != nil {
		log.Warn().Err(err).
			Msg("Could not read the environments to check which are still served. A deleted one is served until a check succeeds.")
		return
	}
	enabled := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if row.Enabled {
			enabled = append(enabled, uuid.UUID(row.ID))
		}
	}
	for _, id := range running {
		if !slices.Contains(enabled, id) {
			a.stopEnvironment(id)
		}
	}
}

// stopEnvironment stops serving one environment and lets go of its database.
func (a *App) stopEnvironment(id uuid.UUID) {
	if !a.environments.stop(id) {
		return
	}
	log.Warn().Str("environment", id.String()).
		Msg("This environment was deleted or disabled. Its port and its workers are stopped, and its database connections close shortly.")
	time.AfterFunc(environmentCloseAfter, func() {
		if db, open := gorms.ForgetEnvironmentDB(id); open {
			closeDB(db)
		}
		if a.storm != nil {
			if pool, open := a.storm.ForgetEnvironment(id); open {
				pool.Close()
			}
		}
	})
}
