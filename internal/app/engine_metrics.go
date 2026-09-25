package app

import (
	"context"
	"time"

	"github.com/gsoultan/metis/internal/pkg/metrics"
	"github.com/gsoultan/metis/server/domains/entities"
)

// engineState reads how far behind the engine is, for the metrics endpoint.
//
// As system work: the job queue and the incidents belong to every tenant at
// once, and the repositories refuse these reads to anything else. Only the
// main database's; a job on an environment's port is in that environment's.
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
