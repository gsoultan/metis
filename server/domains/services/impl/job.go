package impl

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/adapters"
	"github.com/gsoultan/gobpm/server/domains/entities"
	contracts2 "github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/semaphore"
)

// maxConcurrentJobs caps the number of goroutines spawned per polling tick.
// Raising this above the number of DB connections is counter-productive.
const maxConcurrentJobs = 5

type jobService struct {
	repo         repositories.Repository
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
	workerID, _ := uuid.NewV7()
	return &jobService{
		repo:         repo,
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
	fireAt, err := entities.ParseTimerExpression(duration, time.Now())
	if err != nil {
		return fmt.Errorf("timer on node %s: %w", node.ID, err)
	}

	job := entities.Job{
		Instance:   &instance,
		Definition: &entities.ProcessDefinition{ID: instance.Definition.ID},
		Node:       &node,
		Type:       entities.JobTimer,
		Status:     entities.JobPending,
		Payload:    instance.Variables,
		NextRunAt:  fireAt,
	}
	_, err = s.repo.Job().Create(ctx, adapters.JobModelAdapter{Job: job}.ToModel())
	return err
}

func (s *jobService) StartWorkers(ctx context.Context) {
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
	_ = s.dispatchPendingJobs(ctx, false)
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

// tryAcquireJobLock acquires both the DB row-lock and the distributed advisory lock
// so that multiple engine replicas cannot process the same job simultaneously.
func (s *jobService) tryAcquireJobLock(ctx context.Context, jobID uuid.UUID) bool {
	locked, err := s.repo.Job().Lock(ctx, jobID, 5*time.Minute, s.workerID)
	if err != nil {
		log.Error().Err(err).Msg("failed to acquire DB job lock")
		return false
	}
	if !locked {
		return false
	}
	distLocked, err := s.locker.TryAcquire(ctx, "job:"+jobID.String(), 5*time.Minute)
	if err != nil {
		log.Error().Err(err).Msg("failed to acquire distributed job lock")
		return false
	}
	return distLocked
}

func (s *jobService) runJob(ctx context.Context, job entities.Job) {
	log.Info().Str("jobId", job.ID.String()).Str("type", string(job.Type)).Msg("Running job")
	defer func() { _ = s.locker.Release(ctx, "job:"+job.ID.String()) }()

	var err error
	switch job.Type {
	case entities.JobServiceTask:
		err = s.executeServiceTask(ctx, job)
	case entities.JobTimer:
		err = s.executeTimer(ctx, job)
	case entities.JobTimerBoundary:
		err = s.executeTimerBoundary(ctx, job)
	}

	if err != nil {
		log.Error().Err(err).Str("jobId", job.ID.String()).Msg("Job execution failed")
		if s.tryErrorBoundaryRoute(ctx, job, err) {
			job.Status = entities.JobCompleted
		} else {
			s.handleJobFailure(ctx, &job, err)
		}
	} else {
		job.Status = entities.JobCompleted
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
		job.NextRunAt = time.Now().Add(time.Duration(job.Retries) * time.Minute)
		return
	}
	job.Status = entities.JobFailed
	s.createIncident(ctx, job, jobErr)
}

// createIncident persists an open incident record for a permanently failed job.
func (s *jobService) createIncident(ctx context.Context, job *entities.Job, jobErr error) {
	incID, _ := uuid.NewV7()
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
	md, err := s.repo.Definition().Get(ctx, job.Definition.ID)
	if err != nil {
		return err
	}
	def := adapters.DefinitionEntityAdapter{Model: md}.ToEntity()

	node := def.FindNode(job.Node.ID)
	if node == nil {
		return fmt.Errorf("node %s not found", job.Node.ID)
	}

	responseData, err := s.resolveAndExecuteConnector(ctx, def, *node, job.Payload)
	if err != nil {
		return err
	}
	if responseData == nil {
		// No connector matched — try the HTTP runner.
		responseData, err = s.httpRunner.Run(ctx, *node, job.Payload)
		if err != nil {
			return err
		}
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
	fireAt, err := entities.ParseTimerExpression(duration, time.Now())
	if err != nil {
		return fmt.Errorf("boundary timer on node %s: %w", boundaryNode.ID, err)
	}
	job := entities.Job{
		Instance:   &instance,
		Definition: &entities.ProcessDefinition{ID: instance.Definition.ID},
		Node:       &boundaryNode,
		Type:       entities.JobTimerBoundary,
		Status:     entities.JobPending,
		Payload:    instance.Variables,
		NextRunAt:  fireAt,
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
