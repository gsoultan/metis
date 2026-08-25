package impl

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gsoultan/gobpm/internal/pkg/tracing"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/internal/pkg/circuit"
	"github.com/gsoultan/gobpm/internal/pkg/idempotency"
	"github.com/gsoultan/gobpm/internal/pkg/ratelimit"
	"github.com/gsoultan/gobpm/server/domains/adapters"
	"github.com/gsoultan/gobpm/server/domains/entities"
	contracts2 "github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
)

// maxConcurrentJobs caps the number of goroutines spawned per polling tick.
// Raising this above the number of DB connections is counter-productive.
const maxConcurrentJobs = 5

type jobService struct {
	repo repositories.Repository
	// breakers stop the job pool filling with calls to a downstream that is
	// already down — see internal/pkg/circuit.
	breakers *circuit.Group
	// limits keep one process from spending a partner's whole quota — see
	// internal/pkg/ratelimit.
	limits       *ratelimit.Group
	engine       contracts2.ExecutionEngine
	connectorSvc contracts2.ConnectorService
	locker       contracts2.DistributedLocker
	errorMatcher contracts2.ErrorBoundaryMatcher
	workerID     string
	httpRunner   *HTTPServiceTaskRunner
	// sem limits the number of concurrent job goroutines to maxConcurrentJobs.
	sem *semaphore.Weighted
}

func NewJobService(
	repo repositories.Repository,
	engine contracts2.ExecutionEngine,
	connectorSvc contracts2.ConnectorService,
	locker contracts2.DistributedLocker,
	errorMatcher contracts2.ErrorBoundaryMatcher,
) contracts2.JobService {
	// A worker with no id cannot hold a lock that anybody can attribute, so a
	// failure here is worth knowing about even though the engine can carry on
	// with the zero value.
	workerID, err := uuid.NewV7()
	if err != nil {
		log.Warn().Err(err).Msg("Could not generate a worker id; job locks will be harder to attribute")
	}
	return &jobService{
		repo:         repo,
		breakers:     circuit.NewGroup(circuit.DefaultSettings()),
		limits:       ratelimit.NewGroup(ratelimit.DefaultSettings()),
		engine:       engine,
		connectorSvc: connectorSvc,
		locker:       locker,
		errorMatcher: errorMatcher,
		workerID:     workerID.String(),
		httpRunner:   NewHTTPServiceTaskRunner(nil), // uses the shared guarded client
		sem:          semaphore.NewWeighted(maxConcurrentJobs),
	}
}

func (s *jobService) EnqueueServiceTask(ctx context.Context, instance entities.ProcessInstance, node entities.Node, iterationID string) error {
	job := entities.Job{
		IterationID: iterationID,
		Instance:    &instance,
		Definition:  &entities.ProcessDefinition{ID: instance.Definition.ID},
		Node:        &node,
		Type:        entities.JobServiceTask,
		Status:      entities.JobPending,
		Payload:     instance.Variables,
		MaxRetries:  3,
		NextRunAt:   time.Now(),
	}
	_, err := s.repo.Job().Create(ctx, adapters.JobModelAdapter{Job: job}.ToModel())
	return err
}

func (s *jobService) EnqueueTimer(ctx context.Context, instance entities.ProcessInstance, node entities.Node, duration string) error {
	schedule, err := entities.ParseTimerSchedule(duration, time.Now())
	if err != nil {
		return fmt.Errorf("timer on node %s: %w", node.ID, err)
	}

	job := entities.Job{
		Instance:         &instance,
		Definition:       &entities.ProcessDefinition{ID: instance.Definition.ID},
		Node:             &node,
		Type:             entities.JobTimer,
		Status:           entities.JobPending,
		Payload:          instance.Variables,
		NextRunAt:        schedule.FireAt,
		RepeatsRemaining: schedule.Repeats,
	}
	_, err = s.repo.Job().Create(ctx, adapters.JobModelAdapter{Job: job}.ToModel())
	return err
}

func (s *jobService) StartWorkers(ctx context.Context) {
	// The job worker spans every tenant by design — a worker that could only see
	// one organization's jobs would leave the rest unprocessed. Saying so
	// explicitly is what lets a context with no identity at all be treated as a
	// mistake rather than as background work.
	ctx = entities.WithSystemContext(ctx)

	ticker := time.NewTicker(2 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.processPendingJobs(ctx)
			}
		}
	}()
}

// processPendingJobs claims up to maxConcurrentJobs jobs and executes each in a
// goroutine.  A semaphore prevents unbounded goroutine growth when jobs arrive
// faster than they complete.
func (s *jobService) processPendingJobs(ctx context.Context) {
	// The ticker cannot return anything, so a failed round is logged here.
	// Dropping it left a worker that had stopped claiming jobs looking exactly
	// like one with nothing to do.
	if err := s.dispatchPendingJobs(ctx, false); err != nil {
		log.Error().Err(err).Msg("A round of pending jobs could not be dispatched")
	}
}

// ProcessPendingJobs runs one round of pending jobs and waits for it to finish.
//
// The ticker deliberately does not wait, so that a slow job cannot hold up the
// next round. A caller that needs the work actually done before looking at the
// result — a test, or a one-shot drain — has no way to express that other than
// sleeping and hoping, which is how service task execution ended up with no
// coverage at all: the tests substituted a stub for this service rather than
// wait on it.
func (s *jobService) ProcessPendingJobs(ctx context.Context) error {
	return s.dispatchPendingJobs(ctx, true)
}

func (s *jobService) dispatchPendingJobs(ctx context.Context, wait bool) error {
	ms, err := s.repo.Job().GetPending(ctx, maxConcurrentJobs)
	if err != nil {
		log.Error().Err(err).Msg("failed to get pending jobs")
		return err
	}

	var running sync.WaitGroup
	for _, m := range ms {
		if !s.tryAcquireJobLock(ctx, uuid.UUID(m.ID)) {
			continue
		}
		// Acquire a slot before spawning; the slot is released when the job finishes.
		if err := s.sem.Acquire(ctx, 1); err != nil {
			break // context cancelled
		}
		job := adapters.JobEntityAdapter{Model: m}.ToEntity()
		running.Add(1)
		go func(j entities.Job) {
			defer running.Done()
			defer s.sem.Release(1)
			s.runJob(ctx, j)
		}(job)
	}
	if wait {
		running.Wait()
	}
	return nil
}

// tryAcquireJobLock claims a job for this worker, and only this worker.
//
// The **row update is what makes claiming exactly-once**: it moves the job to
// running only `WHERE status = pending OR lock_expires < now`, so of N replicas
// issuing it concurrently exactly one reports a row affected. That holds on
// every supported dialect and needs no coordination outside the database.
//
// The distributed lock is a second, optional gate for deployments that want one
// (Strategy; the default is NoOpLocker, a Null Object). It is taken **first**,
// before the row update, and that order is the point: the row update is durable
// state with a five-minute lease on it, so claiming the row and then failing to
// take the lock would leave a job marked running that nobody is running — idle
// until the lease expired. Reversing the order means a refusal costs nothing,
// because nothing has been written yet.
func (s *jobService) tryAcquireJobLock(ctx context.Context, jobID uuid.UUID) bool {
	lockKey := "job:" + jobID.String()

	distLocked, err := s.locker.TryAcquire(ctx, lockKey, 5*time.Minute)
	if err != nil {
		log.Error().Err(err).Str("jobId", jobID.String()).Msg("failed to acquire distributed job lock")
		return false
	}
	if !distLocked {
		return false
	}

	locked, err := s.repo.Job().Lock(ctx, jobID, 5*time.Minute, s.workerID)
	if err != nil || !locked {
		// Another replica claimed the row, or the claim failed. Either way this
		// worker is not running the job, so it must not keep the lock — holding
		// it would block whichever replica did win from ever releasing.
		if releaseErr := s.locker.Release(ctx, lockKey); releaseErr != nil {
			log.Warn().Err(releaseErr).Str("jobId", jobID.String()).Msg("Could not release the job lock after a lost claim")
		}
		if err != nil {
			log.Error().Err(err).Str("jobId", jobID.String()).Msg("failed to acquire DB job lock")
		}
		return false
	}
	return true
}

func (s *jobService) runJob(ctx context.Context, job entities.Job) {
	log.Info().Str("jobId", job.ID.String()).Str("type", string(job.Type)).Msg("Running job")
	defer func() {
		// A lock that will not release is held until it expires, and every
		// worker that wanted this job waits behind it.
		if err := s.locker.Release(ctx, "job:"+job.ID.String()); err != nil {
			log.Warn().Err(err).Str("jobId", job.ID.String()).Msg("Could not release the job lock")
		}
	}()

	var err error
	switch job.Type {
	case entities.JobServiceTask:
		err = s.executeServiceTask(ctx, job)
	case entities.JobTimer:
		err = s.executeTimer(ctx, job)
	case entities.JobTimerBoundary:
		err = s.executeTimerBoundary(ctx, job)
	}

	if deferred, isDeferral := deferralOf(err); isDeferral {
		// Not a failure: put it back with a time on it and leave the attempt
		// count alone. An error boundary must not catch this either — a rate
		// limit is not something a process models a path for.
		log.Info().
			Str("jobId", job.ID.String()).
			Dur("retryIn", deferred.after).
			Msg(deferred.reason)
		job.Status = entities.JobPending
		job.NextRunAt = time.Now().Add(deferred.after)
	} else if err != nil {
		log.Error().Err(err).Str("jobId", job.ID.String()).Msg("Job execution failed")
		if s.tryErrorBoundaryRoute(ctx, job, err) {
			job.Status = entities.JobCompleted
		} else {
			s.handleJobFailure(ctx, &job, err)
		}
	} else {
		job.Status = entities.JobCompleted
		if job.Type == entities.JobTimer || job.Type == entities.JobTimerBoundary {
			s.rescheduleRepeatingTimer(ctx, &job)
		}
	}

	job.UpdatedAt = time.Now()
	if err := s.repo.Job().Update(ctx, adapters.JobModelAdapter{Job: job}.ToModel()); err != nil {
		log.Error().Err(err).Msg("failed to update job status")
	}
}

// tryErrorBoundaryRoute checks if a matching error boundary event exists for the
// failed job's node and, if found, routes the process through it.
// Returns true if the error was successfully handled by a boundary event.
func (s *jobService) tryErrorBoundaryRoute(ctx context.Context, job entities.Job, jobErr error) bool {
	md, err := s.repo.Definition().Get(ctx, job.Definition.ID)
	if err != nil {
		return false
	}
	def := adapters.DefinitionEntityAdapter{Model: md}.ToEntity()

	for _, boundary := range def.GetBoundaryEvents(job.Node.ID) {
		if !s.errorMatcher.Matches(jobErr, *boundary) {
			continue
		}
		instance, err := s.engine.GetInstance(ctx, job.Instance.ID)
		if err != nil {
			return false
		}
		if boundary.CancelActivity {
			instance.RemoveTokenByNode(boundary)
		}
		if err := s.engine.ExecuteNode(ctx, &instance, def, boundary.ID); err != nil {
			log.Error().Err(err).Str("boundaryNode", boundary.ID).Msg("error boundary execution failed")
			return false
		}
		return true
	}
	return false
}

// handleJobFailure applies retry logic or creates an incident when a job fails
// and no error boundary event caught it.
func (s *jobService) handleJobFailure(ctx context.Context, job *entities.Job, jobErr error) {
	job.Retries++
	job.LastError = jobErr.Error()
	if job.Retries < job.MaxRetries {
		job.Status = entities.JobPending
		// Exponential with jitter — see backoff.go for why the linear schedule
		// this replaces made an outage worse rather than better.
		job.NextRunAt = time.Now().Add(retryDelay(job.Retries))
		return
	}
	job.Status = entities.JobFailed
	s.createIncident(ctx, job, jobErr)
}

// createIncident persists an open incident record for a permanently failed job.
func (s *jobService) createIncident(ctx context.Context, job *entities.Job, jobErr error) {
	incID, err := uuid.NewV7()
	if err != nil {
		log.Error().Err(err).Msg("Could not generate an incident id; the incident was not recorded")
		return
	}
	incident := entities.Incident{
		ID:         incID,
		Job:        job,
		Instance:   &entities.ProcessInstance{ID: job.Instance.ID},
		Definition: &entities.ProcessDefinition{ID: job.Definition.ID},
		Node:       job.Node,
		Error:      jobErr.Error(),
		Status:     entities.IncidentOpen,
		CreatedAt:  time.Now(),
	}
	if _, err := s.repo.Incident().Create(ctx, adapters.IncidentModelAdapter{Incident: incident}.ToModel()); err != nil {
		log.Error().Err(err).Msg("failed to create incident")
	}
}

func (s *jobService) ListIncidents(ctx context.Context, instanceID uuid.UUID) ([]entities.Incident, error) {
	ms, err := s.repo.Incident().ListByInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	res := make([]entities.Incident, len(ms))
	for i, m := range ms {
		res[i] = adapters.IncidentEntityAdapter{Model: m}.ToEntity()
	}
	return res, nil
}

func (s *jobService) ResolveIncident(ctx context.Context, incidentID uuid.UUID) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		m, err := s.repo.Incident().Get(txCtx, incidentID)
		if err != nil {
			return err
		}
		incident := adapters.IncidentEntityAdapter{Model: m}.ToEntity()

		if incident.Status == entities.IncidentResolved {
			return nil
		}

		mj, err := s.repo.Job().Get(txCtx, incident.Job.ID)
		if err != nil {
			return err
		}
		job := adapters.JobEntityAdapter{Model: mj}.ToEntity()

		// Reset job status and retries
		job.Status = entities.JobPending
		job.Retries = 0
		job.NextRunAt = time.Now()

		if err := s.repo.Job().Update(txCtx, adapters.JobModelAdapter{Job: job}.ToModel()); err != nil {
			return err
		}

		// Mark incident as resolved
		incident.Status = entities.IncidentResolved
		resolvedAt := time.Now()
		incident.ResolvedAt = &resolvedAt

		return s.repo.Incident().Update(txCtx, adapters.IncidentModelAdapter{Incident: incident}.ToModel())
	})
}

// executeServiceTask runs a service-task job end-to-end:
//  1. Resolve the process definition and node.
//  2. Attempt a configured connector; fall back to HTTPServiceTaskRunner.
//  3. Persist output variables and advance the process token.
func (s *jobService) executeServiceTask(ctx context.Context, job entities.Job) error {
	// The parent span for one attempt at one service task. The connector span
	// nests under it, so a trace reads as "instance X, node Y, attempt 2, called
	// Salesforce, waited 30s" — which is the question asked of a stuck instance.
	ctx, span := tracing.Tracer().Start(ctx, "job.serviceTask",
		trace.WithAttributes(
			tracing.AttrInstanceID.String(job.Instance.ID.String()),
			tracing.AttrNodeID.String(job.Node.ID),
			tracing.AttrDefinitionID.String(job.Definition.ID.String()),
			// Retries is the count already spent, so this attempt is the next.
			tracing.AttrAttempt.Int(job.Retries+1),
		),
	)
	defer span.End()

	md, err := s.repo.Definition().Get(ctx, job.Definition.ID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "definition lookup failed")
		return err
	}
	def := adapters.DefinitionEntityAdapter{Model: md}.ToEntity()

	node := def.FindNode(job.Node.ID)
	if node == nil {
		return fmt.Errorf("node %s not found", job.Node.ID)
	}

	responseData, err := s.callOnce(ctx, job, def, *node)
	if err != nil {
		return err
	}

	// The row lock is taken for the whole read-modify-write, not just the read.
	//
	// Advancing a node that runs once per item increments a completion counter
	// held in the instance, and up to maxConcurrentJobs of these run at once. An
	// unlocked read let two iterations finishing together both see the same count
	// and both write the next one, losing an increment — the counter then never
	// reached the total and the process waited forever with no error and no
	// incident. The connector call above deliberately stays outside the
	// transaction so the lock is not held across network I/O.
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		instance, err := s.engine.GetInstanceForUpdate(txCtx, job.Instance.ID)
		if err != nil {
			return err
		}
		for k, v := range responseData {
			instance.SetVariable(k, v)
		}
		if len(responseData) > 0 {
			if err := s.engine.UpdateInstance(txCtx, instance); err != nil {
				return err
			}
		}
		// For a node that runs once per item this has to say which iteration
		// finished, or the engine cannot tell which of the node's tokens to
		// retire and the process never moves past it.
		return s.engine.ProceedIteration(txCtx, &instance, def, job.Node.ID, job.IterationID)
	})
}

// deferredError says the work is still wanted, just not yet.
//
// It is not a failure and must not be counted as one. A quota is a normal
// condition — the partner has told us how fast we may go, and going slower is
// compliance, not an error. Counting it against the job's retries would fail an
// instance in three attempts for the crime of being popular.
type deferredError struct {
	after  time.Duration
	reason string
}

func (e *deferredError) Error() string {
	return fmt.Sprintf("%s; retrying in %s", e.reason, e.after.Round(time.Second))
}

// deferralOf returns the deferral in err, if it is one.
func deferralOf(err error) (*deferredError, bool) {
	var deferred *deferredError
	if errors.As(err, &deferred) {
		return deferred, true
	}
	return nil, false
}

// callOnce makes the node's outbound call, at most once per unit of work.
//
// The call cannot share a transaction with the token advance that follows it:
// the call is network I/O and the advance takes a row lock on the instance, and
// holding that lock across a call to someone else's API is how one slow partner
// stops a whole engine. So they are separate — and that separation is precisely
// the defect this closes. A call that succeeded and then failed to commit was
// retried, and the second attempt called again. For an endpoint that charges a
// card, that is a second charge.
//
// Three steps, each committed on its own:
//
//  1. Record the call in flight. If a previous attempt already recorded it as
//     completed, return that response and make no call at all.
//  2. Make the call, carrying an idempotency key derived from the unit of work
//     rather than from the attempt.
//  3. Record the response. Only after this does the caller advance the token, so
//     a failure there costs a repeated advance, not a repeated call.
//
// The window that remains — the call succeeded and step 3 did not — is the one
// no client can close, because a request that never arrived and a response that
// was lost look identical from here. The key is what makes that window safe: the
// downstream sees it twice and answers once.
func (s *jobService) callOnce(
	ctx context.Context,
	job entities.Job,
	def *entities.ProcessDefinition,
	node entities.Node,
) (map[string]any, error) {
	target := serviceCallTarget(node)

	// The quota first, because being over one is not a failure and must not
	// spend an attempt. A call held back here has not been made, has not been
	// recorded, and comes back when the partner says it may.
	if limit := s.rateLimitFor(ctx, def, node); limit > 0 {
		if allowed, wait := s.limits.Take(target, limit); !allowed {
			return nil, &deferredError{
				after:  wait,
				reason: fmt.Sprintf("service task %q is at its configured limit of %g calls a minute for %s", job.Node.ID, limit, target),
			}
		}
	}

	// Then the breaker, before recording anything: a call refused here was never
	// made, and inflating the recorded attempt count with calls that did not
	// happen would mislead whoever reads that table during the incident.
	if allowed, state := s.breakers.Allow(target); !allowed {
		log.Warn().
			Str("instance", job.Instance.ID.String()).
			Str("node", job.Node.ID).
			Str("target", target).
			Str("breaker", state.String()).
			Msg("Refusing a service call: the downstream is failing and the retry will come back with backoff")
		return nil, fmt.Errorf("service task %q: not calling %s, its circuit breaker is %s",
			job.Node.ID, target, state)
	}

	key := idempotency.ForServiceCall(job.Instance.ID, job.Node.ID, job.IterationID)

	record, err := s.repo.ServiceCall().Begin(ctx, models.ServiceCallModel{
		InstanceID:     models.UUID(job.Instance.ID),
		NodeID:         job.Node.ID,
		IterationID:    job.IterationID,
		ProjectID:      projectIDOf(def),
		IdempotencyKey: key,
	})
	if err != nil {
		return nil, err
	}

	if record.Status == models.ServiceCallCompleted {
		log.Info().
			Str("instance", job.Instance.ID.String()).
			Str("node", job.Node.ID).
			Msg("Service call already completed on an earlier attempt; reusing its response")
		return record.Response, nil
	}
	if record.Attempts > 1 {
		// Worth saying out loud: the call is about to be made a second time, and
		// whether that is safe now rests entirely on the downstream honouring
		// the key.
		log.Warn().
			Str("instance", job.Instance.ID.String()).
			Str("node", job.Node.ID).
			Int("attempts", record.Attempts).
			Str("idempotency_key", key).
			Msg("Repeating a service call that did not record a response")
	}

	callCtx := idempotency.WithKey(ctx, key)
	responseData, err := s.resolveAndExecuteConnector(callCtx, def, node, job.Payload)
	if err == nil && responseData == nil {
		// No connector matched — try the HTTP runner.
		responseData, err = s.httpRunner.Run(callCtx, node, job.Payload)
	}
	if err != nil {
		s.breakers.Failed(target)
		return nil, err
	}
	s.breakers.Succeeded(target)

	if err := s.repo.ServiceCall().Complete(ctx, uuid.UUID(record.ID), responseData); err != nil {
		// The call landed. Failing here would retry it, which is the thing this
		// function exists to prevent, so the work goes on and the gap is logged:
		// the next attempt will repeat the call carrying the same key.
		log.Error().Err(err).
			Str("instance", job.Instance.ID.String()).
			Str("node", job.Node.ID).
			Msg("Could not record a completed service call; a retry would repeat it")
	}
	return responseData, nil
}

// rateLimitFor reads the calls-a-minute a target is allowed.
//
// It is read on every call rather than cached: an operator lowering a limit
// because a partner complained should see it take effect on the next request,
// not after a restart. A connector instance's configuration wins, so a limit set
// once on a shared connection covers every node that uses it; a node's own
// property is the fallback for a plain HTTP task, which has no connection to
// hang it on. Zero — the default everywhere — means no limit.
func (s *jobService) rateLimitFor(ctx context.Context, def *entities.ProcessDefinition, node entities.Node) float64 {
	if instance, found, err := s.findConnectorInstance(ctx, def, node); err == nil && found {
		if limit, ok := numericSetting(instance.Config[rateLimitSetting]); ok {
			return limit
		}
	}
	if limit, ok := numericSetting(node.Properties[rateLimitSetting]); ok {
		return limit
	}
	return 0
}

// rateLimitSetting is the name of the calls-a-minute setting, on a connector
// instance's configuration or on a node's properties.
const rateLimitSetting = "rate_limit_per_minute"

// numericSetting reads a number that arrived through JSON, where it may be a
// float, an int or the text an operator typed into a form.
func numericSetting(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, v > 0
	case int:
		return float64(v), v > 0
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return parsed, err == nil && parsed > 0
	default:
		return 0, false
	}
}

// serviceCallTarget names the thing a breaker is about: the downstream, not the
// process step.
//
// A connector instance is the better key when there is one — several nodes can
// share a Salesforce connection, and it is the connection that is unhealthy. For
// a plain HTTP task it is the host, so two tasks calling different paths on the
// same failing API trip one breaker between them rather than one each.
func serviceCallTarget(node entities.Node) string {
	if id := node.GetStringProperty("connector_instance_id"); id != "" {
		return "connector:" + id
	}
	if raw := node.GetStringProperty("http_url"); raw != "" {
		if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
			return "host:" + parsed.Host
		}
		return "url:" + raw
	}
	// Nothing to call, so nothing to break. An empty key is a no-op everywhere
	// in the group.
	return ""
}

// projectIDOf reads the tenant off a definition, so a recorded call scopes like
// every other row.
func projectIDOf(def *entities.ProcessDefinition) models.UUID {
	if def == nil || def.Project == nil {
		return models.NilUUID
	}
	return models.UUID(def.Project.ID)
}

// resolveAndExecuteConnector finds a connector instance for the node and executes
// it if one is configured.  Returns nil, nil when the node configures no
// connector, so the caller can try the HTTP runner instead.
func (s *jobService) resolveAndExecuteConnector(ctx context.Context, def *entities.ProcessDefinition, node entities.Node, payload map[string]any) (map[string]any, error) {
	ci, found, err := s.findConnectorInstance(ctx, def, node)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	connector, err := s.connectorSvc.GetConnector(ctx, ci.Connector.ID)
	if err != nil {
		return nil, fmt.Errorf("connector lookup failed: %w", err)
	}
	result, err := s.connectorSvc.ExecuteConnector(ctx, connector.Key, ci.Config, payload)
	if err != nil {
		return nil, fmt.Errorf("connector execution failed: %w", err)
	}
	return result, nil
}

// findConnectorInstance resolves the connector instance for a node.
// It first tries the explicit connector_instance_id property, then falls back
// to resolving by connector_id within the project.
//
// found is false only when the node names no connector at all. That case is a
// service task which configures nothing, which is a legitimate way to model a
// step that has not been built yet, and the caller falls through to the HTTP
// runner for it.
//
// A node that does name a connector and cannot reach it is an error rather than
// another way of returning false. Falling through there reached the HTTP
// runner, which treats a node with no http_url as a no-op — and a node
// configured to post to Slack has no http_url. Deleting its connector instance
// therefore turned "Send the notification" into a step that notified nobody,
// after which the engine proceeded and the instance finished as completed with
// no incident raised. A process that reports success for work it did not do is
// worse than one that fails, because nothing prompts anyone to look.
func (s *jobService) findConnectorInstance(ctx context.Context, def *entities.ProcessDefinition, node entities.Node) (entities.ConnectorInstance, bool, error) {
	instanceID := node.GetStringProperty("connector_instance_id")
	connectorID := node.GetStringProperty("connector_id")
	if instanceID == "" && connectorID == "" {
		return entities.ConnectorInstance{}, false, nil
	}

	if def == nil || def.Project == nil {
		return entities.ConnectorInstance{}, false, fmt.Errorf(
			"node %q names a connector but its definition has no project to resolve it in", node.ID)
	}

	// Why each candidate was rejected, so the incident says which of the two
	// ways of naming a connector was tried and what went wrong.
	var reasons []string

	if instanceID != "" {
		id, err := uuid.Parse(instanceID)
		switch {
		case err != nil:
			reasons = append(reasons, fmt.Sprintf("connector_instance_id %q is not a valid id", instanceID))
		default:
			ci, err := s.connectorSvc.GetConnectorInstance(ctx, id)
			switch {
			case err != nil:
				reasons = append(reasons, fmt.Sprintf("connector instance %s does not exist", id))
			case ci.Project == nil || ci.Project.ID != def.Project.ID:
				reasons = append(reasons, fmt.Sprintf("connector instance %s belongs to another project", id))
			default:
				return ci, true, nil
			}
		}
	}

	if connectorID != "" {
		id, err := uuid.Parse(connectorID)
		switch {
		case err != nil:
			reasons = append(reasons, fmt.Sprintf("connector_id %q is not a valid id", connectorID))
		default:
			ci, err := s.connectorSvc.GetConnectorInstanceByProjectAndConnector(ctx, def.Project.ID, id)
			if err != nil {
				reasons = append(reasons, fmt.Sprintf("connector %s is not configured for this project", id))
			} else {
				return ci, true, nil
			}
		}
	}

	return entities.ConnectorInstance{}, false, fmt.Errorf(
		"node %q could not reach the connector it is configured with: %s",
		node.ID, strings.Join(reasons, "; "))
}

func (s *jobService) EnqueueBoundaryTimer(ctx context.Context, instance entities.ProcessInstance, boundaryNode entities.Node, duration string) error {
	schedule, err := entities.ParseTimerSchedule(duration, time.Now())
	if err != nil {
		return fmt.Errorf("boundary timer on node %s: %w", boundaryNode.ID, err)
	}
	job := entities.Job{
		Instance:         &instance,
		Definition:       &entities.ProcessDefinition{ID: instance.Definition.ID},
		Node:             &boundaryNode,
		Type:             entities.JobTimerBoundary,
		Status:           entities.JobPending,
		Payload:          instance.Variables,
		NextRunAt:        schedule.FireAt,
		RepeatsRemaining: schedule.Repeats,
	}
	_, err = s.repo.Job().Create(ctx, adapters.JobModelAdapter{Job: job}.ToModel())
	return err
}

// timerTokenBearer returns the node whose tokens decide whether a timer is still
// live.
//
// A boundary event never holds a token of its own — it is a deadline on the
// activity it is attached to, and that activity is where the token sits. A catch
// event does hold its own token.
func timerTokenBearer(def *entities.ProcessDefinition, node *entities.Node) *entities.Node {
	if node == nil {
		return nil
	}
	if node.Type == entities.BoundaryEvent && node.AttachedToRef != "" {
		return def.FindNode(node.AttachedToRef)
	}
	return node
}

// timerStillApplies reports whether a due timer is still relevant to the
// instance it was scheduled for.
//
// A timer cannot be cancelled: JobRepository has no delete, so when a branch of
// an event-based gateway loses the race, or an activity finishes inside its
// deadline, the pending job survives and comes due anyway. Cancelling at the
// source would still leave this check necessary — a job already claimed by a
// worker cannot be recalled — so relevance is decided when the timer fires,
// against the tokens the engine maintains.
func timerStillApplies(instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID, iterationID string) bool {
	if instance.Status != entities.ProcessActive {
		return false
	}
	node := timerTokenBearer(def, def.FindNode(nodeID))
	if node == nil {
		return false
	}
	tokens := instance.GetTokensByNode(node)
	if iterationID == "" {
		return len(tokens) > 0
	}
	// A node that runs once per item has a token per iteration; only the one
	// this job was scheduled for counts.
	for _, tk := range tokens {
		if tk.IterationID == iterationID {
			return true
		}
	}
	return false
}

// rescheduleRepeatingTimer queues the next occurrence of a repeating timer.
//
// BPMN's timeCycle ("R3/PT10M") is what a non-interrupting boundary timer uses
// to nag while an activity runs. Each occurrence is its own job, so firing one
// has to queue the next — and the relevance check in timerStillApplies stops the
// chain naturally once the activity it belongs to has moved on, which is why an
// interrupting timer needs no special case here.
func (s *jobService) rescheduleRepeatingTimer(ctx context.Context, job *entities.Job) {
	if job.RepeatsRemaining == 0 || job.Node == nil {
		return
	}

	md, err := s.repo.Definition().Get(ctx, job.Definition.ID)
	if err != nil {
		log.Error().Err(err).Str("jobId", job.ID.String()).Msg("Cannot reschedule repeating timer: definition unavailable")
		return
	}
	def := adapters.DefinitionEntityAdapter{Model: md}.ToEntity()
	node := def.FindNode(job.Node.ID)
	if node == nil {
		return
	}

	expr := node.GetStringProperty("timer_duration")
	if expr == "" {
		expr = node.Condition
	}
	schedule, err := entities.ParseTimerSchedule(expr, time.Now())
	if err != nil || schedule.Every <= 0 {
		return
	}

	next := *job
	next.ID = uuid.Nil
	next.Status = entities.JobPending
	next.NextRunAt = time.Now().Add(schedule.Every)
	next.LastError = ""
	next.Retries = 0
	if job.RepeatsRemaining > 0 {
		next.RepeatsRemaining = job.RepeatsRemaining - 1
	}

	if _, err := s.repo.Job().Create(ctx, adapters.JobModelAdapter{Job: next}.ToModel()); err != nil {
		log.Error().Err(err).Str("jobId", job.ID.String()).Msg("Cannot reschedule repeating timer")
	}
}

// executeTimerBoundary fires the boundary event node directly on the instance.
func (s *jobService) executeTimerBoundary(ctx context.Context, job entities.Job) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		instance, err := s.engine.GetInstanceForUpdate(txCtx, job.Instance.ID)
		if err != nil {
			return err
		}
		md, err := s.repo.Definition().Get(txCtx, job.Definition.ID)
		if err != nil {
			return err
		}
		def := adapters.DefinitionEntityAdapter{Model: md}.ToEntity()

		// A boundary timer is a deadline on the activity it is attached to. Once
		// that activity has moved on, the deadline is moot — firing it would
		// start the escalation path for work that finished on time.
		if !timerStillApplies(&instance, def, job.Node.ID, job.IterationID) {
			log.Debug().
				Str("instanceId", instance.ID.String()).
				Str("boundaryNodeId", job.Node.ID).
				Msg("Boundary timer came due after its activity had already moved on; skipping")
			return nil
		}

		return s.engine.ExecuteNode(txCtx, &instance, def, job.Node.ID)
	})
}

func (s *jobService) executeTimer(ctx context.Context, job entities.Job) error {
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		instance, err := s.engine.GetInstanceForUpdate(txCtx, job.Instance.ID)
		if err != nil {
			return err
		}

		md, err := s.repo.Definition().Get(txCtx, job.Definition.ID)
		if err != nil {
			return err
		}
		def := adapters.DefinitionEntityAdapter{Model: md}.ToEntity()

		// The token is gone when another branch of an event-based gateway has
		// already won the race. Proceeding anyway would take the losing branch
		// as well and run the process down two paths at once.
		if !timerStillApplies(&instance, def, job.Node.ID, job.IterationID) {
			log.Debug().
				Str("instanceId", instance.ID.String()).
				Str("nodeId", job.Node.ID).
				Msg("Timer came due after its branch was already resolved; skipping")
			return nil
		}

		// For a node that runs once per item this has to say which iteration
		// finished, or the engine cannot tell which of the node's tokens to
		// retire and the process never moves past it.
		return s.engine.ProceedIteration(txCtx, &instance, def, job.Node.ID, job.IterationID)
	})
}
