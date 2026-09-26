package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/rs/zerolog/log"
)

// serveEnvironmentPort serves one environment's port until its context ends,
// then shuts the listener down the way the main one is: a request in flight
// gets the shutdown time to finish.
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
func serveEnvironmentPort(ctx context.Context, name string, listener net.Listener, handler http.Handler) {
	address := listener.Addr().String()
	server := newHTTPServer(address, handler)
	go func() {
		<-ctx.Done()
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

	log.Info().Str("environment", name).Str("addr", address).Msg("Environment listening")
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error().Err(err).
			Str("environment", name).
			Str("addr", address).
			Msg("This environment's port stopped serving. The others are unaffected.")
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

// organizationOfProject answers which tenant a project belongs to, for the
// event stream, which has nobody to tell why it could not.
func (a *App) organizationOfProject(ctx context.Context, projectID uuid.UUID) (uuid.UUID, bool) {
	organization, err := a.projectOrganization(ctx, projectID)
	if err != nil {
		log.Debug().Err(err).Str("project", projectID.String()).
			Msg("Could not resolve a project's organization, so an event was not delivered to any browser.")
		return uuid.Nil, false
	}
	return organization, true
}

// projectOrganization answers which tenant a project belongs to, or says why it
// cannot.
//
// Read under a system context because it is asked on behalf of background work
// that legitimately spans tenants — the point of the question is to find out
// which one, so it cannot be asked from inside one.
func (a *App) projectOrganization(ctx context.Context, projectID uuid.UUID) (uuid.UUID, error) {
	project, err := a.repo.Project().Get(entities.WithSystemContext(ctx), projectID)
	if errors.Is(err, apierr.ErrNotFound) {
		return uuid.Nil, fmt.Errorf("project %s does not exist", projectID)
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("could not read project %s: %w", projectID, err)
	}
	organization := uuid.UUID(project.OrganizationID)
	if organization == uuid.Nil {
		return uuid.Nil, fmt.Errorf("project %s belongs to no organization", projectID)
	}
	return organization, nil
}
