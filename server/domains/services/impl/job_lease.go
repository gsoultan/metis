package impl

import (
	"context"
	"time"

	"github.com/google/uuid"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/rs/zerolog/log"
)

// Holding a job's lease while it runs.
//
// A claim gives the worker the job for one lease. The lease used to be taken
// once and never renewed, so a run that outlasted it — a partner slower than
// the lease, METIS_HTTP_TIMEOUT set above it — was claimed and run again by
// another replica while the first was still waiting on its call: the partner
// was called twice. Now the worker renews the lease while the job runs, and
// every outcome is written only while the worker still holds the job
// (JobRepository.Settle), so a run that loses its lease anyway — a pause long
// enough to miss every renewal — records nothing over the one that took over.

// renewalsPerLease is how many times a lease is renewed within its length: a
// third of it, so that two renewals in a row can fail before it runs out.
const renewalsPerLease = 3

// holdLease renews the lease on jobID until the returned stop is called. When
// the lease is found lost it stops the run through lost, with
// ErrJobLeaseLost as the cause. A renewal that fails for another reason — the
// database briefly unreachable — is retried at the next tick: the lease is
// still the worker's until it runs out.
func (s *jobService) holdLease(ctx context.Context, jobID uuid.UUID, lost context.CancelCauseFunc) (stop func()) {
	every := s.settings.renewEvery()
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			renewCtx, cancel := detach(ctx, statusWriteBudget)
			held, err := s.repo.Job().Renew(renewCtx, jobID, s.workerID, s.settings.Lease)
			cancel()
			if err != nil {
				log.Warn().Err(err).Str("jobId", jobID.String()).Msg("Could not renew the job's lease; trying again")
				continue
			}
			if !held {
				log.Warn().Str("jobId", jobID.String()).Msg("The job's lease ran out before it could be renewed; stopping the run")
				lost(repocontracts.ErrJobLeaseLost)
				return
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}
