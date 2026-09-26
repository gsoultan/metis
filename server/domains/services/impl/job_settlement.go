package impl

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/redaction"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/rs/zerolog/log"
)

// How a job's outcome is recorded.
//
// A job's status used to be written after its work, in a statement of its own
// outside the work's transaction. A worker that died between the two — a
// killed pod, a crash, a status write that failed — left the job marked
// running, and the queue never offered a running job to anybody again, so the
// process behind it hung with no incident and no log line after the one that
// went missing.
//
// Two changes, which only work together. Every outcome that changes a process
// commits the job's status in the same transaction as the change, so a job
// whose work committed can never be found still running. And a job found
// running with its lease expired is reclaimed — which, without the first,
// would have done a finished job's work a second time: the engine's advance
// removes a token that is no longer there without complaint and follows the
// outgoing flows anyway.

// statusWriteBudget bounds the detached writes below. Short: each is a few
// statements by primary key, and a process that is shutting down should not
// hang on them.
const statusWriteBudget = 5 * time.Second

// errWorkerStopped is the failure recorded for the attempts a job lost to
// workers that died while running it.
var errWorkerStopped = errors.New("the worker running this step stopped before it finished, more than once")

// minimumReclaims is how many workers a job may lose before it is treated as
// what killed them. A floor, because a worker usually dies for reasons that
// are not the job's — a deploy that outran its drain, another job's script
// running the pod out of memory — and a timer has no retries of its own, so
// without one a single restart would turn a timer into an incident.
const minimumReclaims = 3

// completeJob marks a job done, inside the transaction that did its work. The
// write clears the lease, as every status write does: the entity carries none.
func (s *jobService) completeJob(ctx context.Context, job entities.Job) error {
	job.Status = entities.JobCompleted
	job.UpdatedAt = time.Now()
	return s.repo.Job().Update(ctx, adapters.JobModelAdapter{Job: job}.ToModel())
}

// exhaustedByReclaim accounts for a job reclaimed from a worker that died, and
// reports whether that used up its attempts — in which case it has been failed
// with an incident rather than run again.
//
// Lock has already counted the lost attempt in the row; this keeps the copy in
// hand in step with it. A job that kills its worker every time — a script that
// runs a pod out of memory — would otherwise be picked up by the next pod, and
// the next, for as long as there were pods.
func (s *jobService) exhaustedByReclaim(ctx context.Context, job *entities.Job) bool {
	if job.Status != entities.JobRunning {
		return false
	}
	job.Retries++
	log.Warn().
		Str("jobId", job.ID.String()).
		Int("attempt", job.Retries).
		Msg("Reclaimed a job whose worker stopped before finishing it")
	if job.Retries < max(job.MaxRetries, minimumReclaims) {
		return false
	}
	s.failJob(ctx, *job, errWorkerStopped)
	return true
}

// deferJob puts a job back with a time on it, leaving its attempts alone: a
// rate limit is the partner saying "not yet", not a failure.
func (s *jobService) deferJob(ctx context.Context, job entities.Job, deferred *deferredError) {
	log.Info().
		Str("jobId", job.ID.String()).
		Dur("retryIn", deferred.after).
		Msg(deferred.reason)
	job.Status = entities.JobPending
	job.NextRunAt = time.Now().Add(deferred.after)
	s.writeDetached(ctx, job)
}

// recordFailure retries a failed job or, with its attempts used up, fails it
// with an incident.
func (s *jobService) recordFailure(ctx context.Context, job entities.Job, jobErr error) {
	// The worker stopping is not the job's failure. Counting it would let a
	// few deploys in a row turn a healthy job into an incident.
	if ctx.Err() != nil {
		job.Status = entities.JobPending
		job.NextRunAt = time.Now()
		s.writeDetached(ctx, job)
		return
	}
	job.Retries++
	if job.Retries < job.MaxRetries {
		job.Status = entities.JobPending
		// Redacted: the job row is as durable as the incident, and a connector
		// error carries the URL it called, query-string credentials included.
		job.LastError = redaction.RedactText(jobErr.Error())
		// Exponential with jitter — see backoff.go for why the linear schedule
		// this replaces made an outage worse rather than better.
		job.NextRunAt = time.Now().Add(retryDelay(job.Retries))
		s.writeDetached(ctx, job)
		return
	}
	s.failJob(ctx, job, jobErr)
}

// failJob marks a job failed and raises its incident, together: a failed job
// with no incident is invisible, and an incident whose job still looks
// runnable would be raised again by the next attempt.
func (s *jobService) failJob(ctx context.Context, job entities.Job, jobErr error) {
	job.Status = entities.JobFailed
	job.LastError = redaction.RedactText(jobErr.Error())
	job.UpdatedAt = time.Now()
	writeCtx, cancel := detach(ctx, statusWriteBudget)
	defer cancel()
	err := s.repo.UnitOfWork().Do(writeCtx, func(txCtx context.Context) error {
		if err := s.createIncident(txCtx, &job, jobErr); err != nil {
			return err
		}
		return s.repo.Job().Update(txCtx, adapters.JobModelAdapter{Job: job}.ToModel())
	})
	if err != nil {
		log.Error().Err(err).Str("jobId", job.ID.String()).
			Msg("Could not record the job as failed; it stays running and is tried again when its lease expires")
	}
}

// writeDetached records a job's status in a statement of its own, for the
// outcomes that change nothing else.
//
// Detached: if the process is shutting down, ctx is already cancelled and the
// write would fail — and a job left running now waits out its lease and is
// reclaimed, which counts an attempt it never lost.
func (s *jobService) writeDetached(ctx context.Context, job entities.Job) {
	job.UpdatedAt = time.Now()
	writeCtx, cancel := detach(ctx, statusWriteBudget)
	defer cancel()
	if err := s.repo.Job().Update(writeCtx, adapters.JobModelAdapter{Job: job}.ToModel()); err != nil {
		log.Error().Err(err).Str("jobId", job.ID.String()).
			Msg("Could not record the job's status; it stays running and is tried again when its lease expires")
	}
}

// tryErrorBoundaryRoute routes a failed job's process through the error
// boundary event that catches the failure, if there is one, and reports
// whether it did.
//
// The route and the job's completion are one transaction, taken on the locked
// instance: the route used to read the instance without a lock and commit on
// its own, then leave the job to be marked afterwards.
func (s *jobService) tryErrorBoundaryRoute(ctx context.Context, job entities.Job, jobErr error) bool {
	def, err := s.engine.GetProcessDefinition(ctx, job.Definition.ID)
	if err != nil {
		return false
	}
	boundary := s.matchingErrorBoundary(def, job.Node.ID, jobErr)
	if boundary == nil {
		return false
	}
	err = s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		instance, err := s.engine.GetInstanceForUpdate(txCtx, job.Instance.ID)
		if err != nil {
			return err
		}
		// An earlier attempt took this route and its worker died before the
		// job said so: the token has already left.
		if !tokenWaitsAt(&instance, def.FindNode(job.Node.ID), job.IterationID) {
			return s.completeJob(txCtx, job)
		}
		if boundary.CancelActivity {
			instance.RemoveTokenByNode(boundary)
		}
		if err := s.engine.ExecuteNode(txCtx, &instance, def, boundary.ID); err != nil {
			return err
		}
		return s.completeJob(txCtx, job)
	})
	if err != nil {
		log.Error().Err(err).Str("boundaryNode", boundary.ID).Msg("error boundary execution failed")
		return false
	}
	return true
}

// matchingErrorBoundary returns the error boundary event on nodeID that
// catches jobErr, or nil.
func (s *jobService) matchingErrorBoundary(def *entities.ProcessDefinition, nodeID string, jobErr error) *entities.Node {
	for _, boundary := range def.GetBoundaryEvents(nodeID) {
		if s.errorMatcher.Matches(jobErr, *boundary) {
			return boundary
		}
	}
	return nil
}

// createIncident records an open incident for a job that has failed for good.
func (s *jobService) createIncident(ctx context.Context, job *entities.Job, jobErr error) error {
	incID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	incident := entities.Incident{
		ID:         incID,
		Job:        job,
		Instance:   &entities.ProcessInstance{ID: job.Instance.ID},
		Definition: &entities.ProcessDefinition{ID: job.Definition.ID},
		Node:       job.Node,
		// Redacted, because this text is the one an operator actually reads: it
		// is stored in the incident table and shown in the UI. A connector
		// failure carries the URL it was calling, and Go's *url.Error includes
		// the query string — so a manifest that puts an API key in a query
		// parameter, which many APIs require, wrote that key into the database
		// in plaintext every time the call failed to connect.
		Error:     redaction.RedactText(jobErr.Error()),
		Status:    entities.IncidentOpen,
		CreatedAt: time.Now(),
	}
	_, err = s.repo.Incident().Create(ctx, adapters.IncidentModelAdapter{Incident: incident}.ToModel())
	return err
}

// tokenWaitsAt reports whether the instance still has the token a job was
// scheduled for: active, and a token on node — the one for this iteration, on
// a node that runs once per item.
func tokenWaitsAt(instance *entities.ProcessInstance, node *entities.Node, iterationID string) bool {
	if instance.Status != entities.ProcessActive || node == nil {
		return false
	}
	tokens := instance.GetTokensByNode(node)
	if iterationID == "" {
		return len(tokens) > 0
	}
	for _, tk := range tokens {
		if tk.IterationID == iterationID {
			return true
		}
	}
	return false
}
