package app

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/metrics"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/db"
)

// engineSources lists the databases whose backlog the metrics endpoint
// reports: the main one, and every environment this replica is meant to serve.
//
// Every environment the last check found enabled, not only the ones running:
// one that could not be opened has jobs nobody will claim, and reporting it
// down is what lets an alert name it. It has no probe then — the watcher has
// already said why it could not start.
func (a *App) engineSources() []metrics.EngineSource {
	environments := a.environments.enabledEnvironments()
	sources := make([]metrics.EngineSource, 0, 1+len(environments))
	sources = append(sources, metrics.EngineSource{Probe: a.engineState})
	for _, environment := range environments {
		source := metrics.EngineSource{Environment: environment.id.String(), EnvironmentName: environment.name}
		if environment.running {
			source.Probe = a.environmentEngineState(environment.id)
		}
		sources = append(sources, source)
	}
	return sources
}

// environmentEngineState reads one environment's backlog, from its database.
func (a *App) environmentEngineState(id uuid.UUID) metrics.EngineProbe {
	return func(ctx context.Context) (metrics.EngineState, error) {
		return a.engineState(db.Bind(ctx, id))
	}
}

// engineState reads how far behind the engine is, in the database the context
// is bound to: the main one, or an environment's.
//
// As system work: the job queue and the incidents belong to every tenant at
// once, and the repositories refuse these reads to anything else.
func (a *App) engineState(ctx context.Context) (metrics.EngineState, error) {
	ctx = entities.WithSystemContext(ctx)
	now := time.Now()
	backlog, err := a.repo.Job().Backlog(ctx, now)
	if err != nil {
		return metrics.EngineState{}, err
	}
	open, err := a.repo.Incident().CountOpen(ctx)
	if err != nil {
		return metrics.EngineState{}, err
	}
	state := metrics.EngineState{DueJobs: backlog.Due, LeaseExpiredJobs: backlog.LeaseExpired, OpenIncidents: open}
	if !backlog.OldestDue.IsZero() {
		state.OldestDueAge = now.Sub(backlog.OldestDue)
	}
	return state, nil
}
