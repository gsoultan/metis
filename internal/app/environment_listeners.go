package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/gorms"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

// serveEnvironments starts one listener per open environment.
//
// The port is how a caller says which runtime they are working in, and it is
// the only way they can say it. An environment named in a header or a body
// would be a value the caller chooses, and choosing production from a staging
// session is exactly what separate environments exist to prevent — so the
// binding is applied by the listener that accepted the connection, before any
// handler sees the request, and nothing downstream can change it.
//
// Each listener serves the same handler as the main port. That is deliberate:
// staging is not a reduced Metis, it is the same Metis over a different
// database. What differs is only where the runtime tables are read from, which
// is what the environment binding decides.
//
// A port that will not bind is reported and skipped. One environment's port
// being taken must not stop the others — least of all production, over a
// development listener somebody pointed at a port already in use.
func (a *App) serveEnvironments(ctx context.Context, g *errgroup.Group, handler http.Handler) {
	rows, err := a.repo.Environment().ListAll(entities.WithSystemContext(ctx))
	if err != nil {
		log.Error().Err(err).Msg("Could not read the environments to serve. None will be served on their own port.")
		return
	}

	for _, row := range rows {
		id := uuid.UUID(row.ID)
		if !row.Enabled {
			continue
		}
		if _, open := gorms.EnvironmentDB(id); !open {
			// Its database could not be opened at boot, and openEnvironments
			// has already said so with the reason. Serving the port anyway
			// would answer every request with ErrEnvironmentUnavailable, which
			// looks like the environment is broken rather than absent.
			log.Warn().
				Str("environment", row.Name).
				Int("port", row.Port).
				Msg("This environment has no database connection, so its port will not be served.")
			continue
		}

		address := fmt.Sprintf(":%d", row.Port)
		name := row.Name
		// Stopped with the rest of the environment when it is deleted or
		// disabled, not only when the server shuts down.
		environmentCtx := a.environments.run(ctx, id)
		g.Go(func() error {
			log.Info().
				Str("environment", name).
				Str("addr", address).
				Msg("Environment listening")
			server := newHTTPServer(address, environmentHandler(id, handler))

			go func() {
				<-environmentCtx.Done()
				shutdownCtx, cancel := context.WithTimeoutCause(
					context.WithoutCancel(ctx),
					httpShutdownTimeout,
					fmt.Errorf("the %s environment's server did not shut down in time", name),
				)
				defer cancel()
				if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
					log.Error().Err(err).Str("environment", name).Msg("Environment server shutdown failed")
				}
			}()

			if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				// Returned as a log line rather than to the group: an
				// errgroup.Go that returns an error cancels the context every
				// other listener shares, so a port collision on staging would
				// take down the main server and production with it.
				log.Error().Err(err).
					Str("environment", name).
					Str("addr", address).
					Msg("This environment's port could not be served. The others are unaffected.")
			}
			return nil
		})
	}
}

// environmentHandler binds every request it passes on to one environment.
//
// Both bindings are applied, because the port is mid-flight: the GORM
// repositories read entities.EnvironmentFrom and the storm ones read
// db.EnvironmentFrom, and a request that carried only one of them would reach
// the right database through half the code and the main database through the
// other half — which is the data mixing this whole design exists to prevent.
func environmentHandler(id uuid.UUID, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := db.Bind(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// startEnvironmentWorkers runs the background work once per open environment.
//
// A job, a timer and an SSE broadcast all live in the database of the runtime
// they belong to. The main process's workers poll the main database, so without
// this a process instance started on the staging port would sit there: its
// timers would never fire, its jobs would never run, its service tasks would
// never retry — and nothing would be logged, because from the worker's point of
// view there was no work.
//
// One set per environment rather than one worker that sweeps all of them. The
// poll interval, the lease and the worker count are settings about a database's
// capacity, and an environment on a laptop should not be sized by production's
// numbers. Separate workers also mean a runtime whose database goes away stops
// only its own work.
func (a *App) startEnvironmentWorkers(ctx context.Context) {
	ids := gorms.OpenEnvironmentIDs()
	if len(ids) == 0 {
		return
	}
	for _, id := range ids {
		// Both bindings, for the reason environmentHandler applies both: the
		// worker reads through the GORM repositories today and the storm ones
		// as the port lands, and a worker that carried only one binding would
		// poll one database and write to another.
		environmentCtx := db.Bind(a.environments.run(ctx, id), id)
		a.svc.StartWorkers(environmentCtx)
		// No second SSE fan-out. The bus is one table in the main database and
		// every row on it carries the environment it belongs to, so one reader
		// serves every runtime — and a per-environment reader would deliver
		// each event twice to the browsers it is for.
	}
	log.Info().Int("environments", len(ids)).Msg("Background workers started per environment")
}

// organizationOfProject answers which tenant a project belongs to.
//
// Read under a system context because it is asked on behalf of background work
// that legitimately spans tenants — the point of the question is to find out
// which one, so it cannot be asked from inside one.
func (a *App) organizationOfProject(ctx context.Context, projectID uuid.UUID) (uuid.UUID, bool) {
	project, err := a.repo.Project().Get(entities.WithSystemContext(ctx), projectID)
	if err != nil {
		log.Debug().Err(err).Str("project", projectID.String()).
			Msg("Could not resolve a project's organization, so an event was not delivered to any browser.")
		return uuid.Nil, false
	}
	organization := uuid.UUID(project.OrganizationID)
	return organization, organization != uuid.Nil
}
