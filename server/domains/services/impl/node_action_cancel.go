package impl

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// cancel ends an instance where it stands, and returns what that did.
//
// locked is the row its caller locked: it is the one written back, so nothing
// that landed before the lock is undone.
//
// It runs in the unit of work its caller opened, and has to: the task rows it
// holds (heldOpen, holdRows) are held only until a transaction ends, and
// nothing here can tell whether one is open.
//
// The tasks are withdrawn, announced and recorded from their rows as they are
// once held (heldOpen), not as a read before that found them: a claim does not
// take the instance, so it is not kept out by the caller's lock, and a task
// claimed as the instance was cancelled was withdrawn from somebody the record
// called nobody and nobody told.
//
// Work parked for outside workers is withdrawn too (withdrawParked). It is a
// row of its own, like a task, and it used to outlive the instance: a worker
// was still handed it, and its report moved the cancelled instance on.
//
// The incidents the instance has open are closed (closeIncidents): there is
// nothing left on it for anybody to decide.
//
// Tokens are cleared and the status is set rather than the rows deleted: what
// this instance did, and how far it got, is the record somebody will ask for.
// Pending timers and queued calls are left alone deliberately — JobRepository
// has no delete. timerStillApplies refuses to fire a timer for an instance
// that is not active, and a queued call asks whether its instance has ended
// before it is made (jobService.executeServiceTask).
func (a nodeActions) cancel(ctx context.Context, locked models.ProcessInstanceModel) (cancellation, error) {
	instanceID := uuid.UUID(locked.ID)
	open, err := a.heldOpen(ctx, instanceID)
	if err != nil {
		return cancellation{}, err
	}
	var withdrawn []models.TaskModel
	for _, task := range open {
		if err := a.repo.Task().UpdateStatus(ctx, uuid.UUID(task.ID), models.TaskCanceled); err != nil {
			return cancellation{}, err
		}
		a.announceWithdrawal(ctx, task)
		withdrawn = append(withdrawn, task)
	}
	parked, err := a.withdrawParked(ctx, locked)
	if err != nil {
		return cancellation{}, err
	}
	// Waiting events go with it. An instance that will never run again
	// cannot honour a subscription, and leaving one means a message arrives
	// later and correlates to something that has stopped.
	subs, err := a.repo.Subscription().ListByInstance(ctx, instanceID)
	if err != nil {
		return cancellation{}, err
	}
	for _, sub := range subs {
		if err := a.repo.Subscription().Delete(ctx, uuid.UUID(sub.ID)); err != nil {
			return cancellation{}, err
		}
	}
	closed, err := a.closeIncidents(ctx, instanceID)
	if err != nil {
		return cancellation{}, err
	}
	locked.Tokens = nil
	locked.Status = models.ProcessCancelled
	if err := a.repo.Process().Update(ctx, locked); err != nil {
		return cancellation{}, err
	}
	return cancellation{instance: locked, withdrawn: withdrawn, parkedWithdrawn: parked, incidentsClosed: closed}, nil
}

// closeIncidents closes the incidents an instance has open, and returns their
// ids in order.
//
// An incident asks somebody to look at an instance and decide. A cancelled
// instance has nothing left to decide, and an incident left open on it stays
// in the inbox for good.
//
// Each is closed by setting its status and when — not by resolving it.
// Resolving an incident does something for the instance: it puts the failed
// job back to be tried, or offers the parked work to a worker again, and
// neither is wanted for an instance that has ended. What the incident says
// went wrong is left as it is: that is the evidence.
//
// Called with the instance held. A job that fails for good holds the instance
// while it decides whether to raise an incident (jobService.failJob), and a
// worker that gives up holds it too, so an incident is either here to close
// or is not raised.
func (a nodeActions) closeIncidents(ctx context.Context, instanceID uuid.UUID) ([]uuid.UUID, error) {
	incidents, err := a.repo.Incident().ListByInstance(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("reading the incidents on instance %s: %w", instanceID, err)
	}
	var closed []uuid.UUID
	now := time.Now()
	for _, incident := range incidents {
		if incident.Status != models.IncidentOpen {
			continue
		}
		incident.Status = models.IncidentResolved
		incident.ResolvedAt = &now
		if err := a.repo.Incident().Update(ctx, incident); err != nil {
			return nil, fmt.Errorf("closing incident %s of instance %s: %w", uuid.UUID(incident.ID), instanceID, err)
		}
		closed = append(closed, uuid.UUID(incident.ID))
	}
	slices.SortFunc(closed, byID)
	return closed, nil
}

// withdrawParked takes the work an instance has parked for outside workers
// off the list they fetch from, and returns how many pieces it took.
//
// It is the engine's own withdrawal (Engine.withdrawExternalTasksOn), the one
// an ad-hoc sub-process uses on the steps still running inside it when it
// finishes: the rows are deleted, and each step that had work parked gets a
// line on the instance's trail saying it was withdrawn, since a row that is
// gone reads otherwise as work somebody did. Called with the instance held,
// which is the order everything that takes both takes them in.
//
// An instance with nothing parked costs the one read that finds that out. One
// with work parked, in a wiring whose engine cannot withdraw it, is refused:
// leaving the work on the list is the defect this closes, and nothing on the
// execution path is silent.
func (a nodeActions) withdrawParked(ctx context.Context, locked models.ProcessInstanceModel) (int, error) {
	instanceID := uuid.UUID(locked.ID)
	parked, err := a.repo.ExternalTask().ListByProcessInstance(ctx, instanceID)
	if err != nil {
		return 0, fmt.Errorf("reading the work instance %s has parked for workers: %w", instanceID, err)
	}
	if len(parked) == 0 {
		return 0, nil
	}
	if a.parked == nil {
		return 0, fmt.Errorf("instance %s has work parked for workers, and this server was wired without the engine that withdraws it", instanceID)
	}
	def, err := a.engine.GetProcessDefinition(ctx, uuid.UUID(locked.DefinitionID))
	if err != nil {
		return 0, fmt.Errorf("reading the process instance %s runs, to withdraw its parked work: %w", instanceID, err)
	}
	steps := make([]*entities.Node, 0, len(parked))
	seen := make(map[string]bool, len(parked))
	for _, task := range parked {
		if seen[task.NodeID] {
			continue
		}
		seen[task.NodeID] = true
		step := def.FindNode(task.NodeID)
		if step == nil {
			// Work parked on a step the graph no longer has is still withdrawn.
			step = &entities.Node{ID: task.NodeID}
		}
		steps = append(steps, step)
	}
	instance := adapters.InstanceEntityAdapter{Model: locked}.ToEntity()
	if err := a.parked.withdrawExternalTasksOn(ctx, &instance, steps); err != nil {
		return 0, err
	}
	return len(parked), nil
}
