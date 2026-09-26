package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/redaction"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/rs/zerolog/log"
)

// serveEnvironments serves every enabled environment on its own port, with its
// own workers, and keeps doing so as environments are created, changed and
// removed: every environmentWatchEvery, on every replica.
//
// Boot is the first of those checks rather than a path of its own, so what
// serving an environment means cannot drift between the two: one created
// while the server runs is opened, migrated, bound and worked exactly as one
// that was there when it started.
func (a *App) serveEnvironments(ctx context.Context, handler http.Handler) {
	ctx = entities.WithSystemContext(ctx)
	a.syncEnvironments(ctx, handler)
	a.environments.work.Go(func() {
		ticker := time.NewTicker(environmentWatchEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.syncEnvironments(ctx, handler)
			}
		}
	})
}

// syncEnvironments brings what this replica serves into line with the
// registry: an environment deleted or disabled is stopped, one whose port or
// database changed is stopped to be started again, and one that is enabled
// and not served is started.
func (a *App) syncEnvironments(ctx context.Context, handler http.Handler) {
	rows, err := a.repo.Environment().ListAll(ctx)
	if err != nil {
		log.Warn().Err(err).
			Msg("Could not read the environments. None is started or stopped until a check succeeds.")
		return
	}
	enabled := make(map[uuid.UUID]models.EnvironmentModel, len(rows))
	for _, row := range rows {
		if row.Enabled {
			enabled[uuid.UUID(row.ID)] = row
		}
	}
	for _, id := range a.environments.runningIDs() {
		if _, ok := enabled[id]; !ok {
			a.stopEnvironment(id, "This environment was deleted or disabled. Its port and its workers are stopped, and its database connections close shortly.")
		}
	}
	for id, row := range enabled {
		a.syncEnvironment(ctx, handler, id, row)
	}
	a.environments.forgetFailuresExcept(enabled)
}

// syncEnvironment starts one enabled environment, or stops it when what it
// runs with is no longer what its row says. A stopped one is started again by
// a later check, once its old connections have closed: work in flight on the
// old database finishes there instead of moving to the new one half way.
func (a *App) syncEnvironment(ctx context.Context, handler http.Handler, id uuid.UUID, row models.EnvironmentModel) {
	settings, err := settingsOf(row)
	current, running := a.environments.serving(id)
	switch {
	case running && err == nil && current == settings:
		a.environments.rename(id, row.Name)
	case running:
		a.stopEnvironment(id, "This environment's port or database changed. It is stopped, and served again with the new settings once its connections have closed.")
	case err != nil:
		a.reportStartFailure(row, err)
	default:
		a.startEnvironment(ctx, handler, row, settings)
	}
}

// startEnvironment starts one environment in the background, so one whose
// database is slow to answer — or whose schema another replica is still
// migrating — holds up only itself. A failure is retried at the next check.
func (a *App) startEnvironment(ctx context.Context, handler http.Handler, row models.EnvironmentModel, settings environmentSettings) {
	id := uuid.UUID(row.ID)
	if !a.environments.claim(id) {
		return
	}
	a.environments.work.Go(func() {
		runtime, err := a.runEnvironment(ctx, handler, row, settings)
		if err != nil {
			a.environments.settled(id)
			if ctx.Err() == nil {
				a.reportStartFailure(row, err)
			}
			return
		}
		a.environments.started(id, runtime)
		a.environments.clearFailure(id)
		log.Info().Str("environment", row.Name).Int("port", row.Port).Str("driver", row.Driver).
			Msg("Environment serving")
	})
}

// runEnvironment opens one environment's database, binds its port, and starts
// its workers and its listener. All of it or none of it: a part that fails
// undoes the ones before it, so an environment is never half served.
func (a *App) runEnvironment(ctx context.Context, handler http.Handler, row models.EnvironmentModel, settings environmentSettings) (*environmentRuntime, error) {
	id := uuid.UUID(row.ID)
	if err := a.openEnvironmentDatabases(ctx, row); err != nil {
		return nil, err
	}
	// The port after the database: bound, it accepts connections, and it
	// should not accept one while there is nothing yet to serve it — a
	// migration can take a while, and a port that answers only at the TCP
	// level looks up to anything that checks it.
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", fmt.Sprintf(":%d", row.Port))
	if err != nil {
		a.closeEnvironmentConnections(id)
		return nil, fmt.Errorf("its port could not be bound: %w", err)
	}
	environmentCtx, stop := context.WithCancel(ctx)
	// Workers of its own, polling its own database: a timer or a job started
	// on this port lives there, where the main workers never look. One set per
	// environment rather than one worker sweeping them all, so a runtime whose
	// database goes away stops only its own work. No second SSE fan-out: the
	// bus is one table in the main database, and every row on it names the
	// environment it belongs to.
	//
	// Both bindings, for the reason environmentHandler applies both: a worker
	// that carried only one would poll one database and write to another.
	a.svc.StartWorkers(db.Bind(environmentCtx, id))
	go serveEnvironmentPort(environmentCtx, row.Name, listener, environmentHandler(id, handler))
	return &environmentRuntime{name: row.Name, settings: settings, stop: stop}, nil
}

// stopEnvironment stops serving one environment and, once a request that was
// in flight has had the listener's shutdown time to finish, lets go of its
// database. Until then it is settling, and is not started again.
func (a *App) stopEnvironment(id uuid.UUID, why string) {
	runtime, ok := a.environments.take(id)
	if !ok {
		return
	}
	runtime.stop()
	log.Warn().Str("environment", runtime.name).Str("environment_id", id.String()).Msg(why)
	time.AfterFunc(environmentCloseAfter, func() {
		a.closeEnvironmentConnections(id)
		a.environments.settled(id)
	})
}

// reportStartFailure says why an environment could not start — at error level
// once for each cause, and at debug level while the same cause lasts. The
// watcher tries again at every check, and a line every fifteen seconds that
// says nothing new is how the line that does gets missed.
func (a *App) reportStartFailure(row models.EnvironmentModel, err error) {
	cause := redaction.RedactError(err)
	event := log.Debug()
	if a.environments.failed(uuid.UUID(row.ID), cause) {
		event = log.Error()
	}
	event.Str("error", cause).Str("environment", row.Name).Int("port", row.Port).
		Msg("This environment could not be started, so it is not served; the others are unaffected. It is tried again at every check, and this is logged again only if the reason changes.")
}
