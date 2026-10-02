package impl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/redaction"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceContracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/rs/zerolog/log"
)

type externalTaskService struct {
	repo   repositories.Repository
	engine serviceContracts.ExecutionEngine
}

func NewExternalTaskService(
	repo repositories.Repository,
	engine serviceContracts.ExecutionEngine,
) serviceContracts.ExternalTaskService {
	return &externalTaskService{
		repo:   repo,
		engine: engine,
	}
}

func (s *externalTaskService) FetchAndLock(ctx context.Context, topic string, workerID string, maxTasks int, lockDuration int64) ([]*entities.ExternalTask, error) {
	ms, err := s.repo.ExternalTask().FetchAndLock(ctx, topic, workerID, maxTasks, lockDuration)
	if err != nil {
		return nil, err
	}
	res := make([]*entities.ExternalTask, len(ms))
	for i, m := range ms {
		task := adapters.ExternalTaskEntityAdapter{Model: *m}.ToEntity()
		res[i] = &task
	}
	return res, nil
}

func (s *externalTaskService) Complete(ctx context.Context, taskID uuid.UUID, workerID string, variables map[string]any) error {
	withdrawn := false
	err := s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		m, err := s.repo.ExternalTask().Get(txCtx, taskID)
		if err != nil {
			return err
		}

		// Then hold the instance, and ask for the task again.
		//
		// The instance first and the task's row second, which is the order the
		// engine takes them in when a step ends and it withdraws the work still
		// parked for it (Engine.endActivity). This used to delete the row and
		// then wait for the instance; a completion that ended the step held the
		// instance and waited to withdraw that row. Each held what the other
		// wanted, the database ended one of them as a deadlock, and a worker was
		// told its report had failed.
		//
		// Read again because the wait is where the task goes: withdrawn by the
		// completion that ended its step, or completed by an earlier report of
		// the same work. Either way it is no longer there to report on, and the
		// worker is told so rather than counted.
		//
		// It is written back whole below, too: two branches whose workers
		// finished at the same moment each wrote a token list without the
		// other's progress in it, and the join then waited for a branch that
		// had finished.
		instance, err := s.engine.GetInstanceForUpdate(txCtx, uuid.UUID(m.ProcessInstanceID))
		if err != nil {
			return err
		}
		if m, err = s.repo.ExternalTask().Get(txCtx, taskID); err != nil {
			return err
		}
		task := adapters.ExternalTaskEntityAdapter{Model: *m}.ToEntity()

		if err := refuseWorkerWithoutLock(task.WorkerID, workerID); err != nil {
			return err
		}
		if task.LockExpiration != nil && task.LockExpiration.Before(time.Now()) {
			return apierr.Invalidf("the lock on this task has run out, and the task may already be with another " +
				"worker — fetch it again")
		}

		// 1. The definition the instance is running.
		def, err := s.engine.GetProcessDefinition(txCtx, instance.Definition.ID)
		if err != nil {
			return err
		}

		// Work for a step that has ended is withdrawn, not counted.
		if withdrawn, err = s.withdrawnBecauseStepEnded(txCtx, &instance, def, task.Node.ID); err != nil || withdrawn {
			return err
		}

		// 2. DeleteConnectorInstance external task
		if err := s.repo.ExternalTask().Delete(txCtx, taskID); err != nil {
			return err
		}

		// 3. UpdateConnectorInstance variables
		for k, v := range variables {
			instance.SetVariable(k, v)
		}
		if err := s.engine.UpdateInstance(txCtx, instance); err != nil {
			return err
		}

		// 4. Continue execution
		log.Info().
			Str("instance_id", task.ProcessInstance.ID.String()).
			Str("node_id", task.Node.ID).
			Msg("Completing external task and continuing execution")

		return s.engine.Proceed(txCtx, &instance, def, task.Node.ID)
	})
	if err != nil {
		return err
	}
	if withdrawn {
		return errExternalTaskWithdrawn()
	}
	return nil
}

// errExternalTaskWithdrawn answers a worker whose report found its step over:
// there is no such task. It is what the report would have been told had the
// work been withdrawn a moment earlier, when the step ended, and what the same
// report sent again is told by the repository.
func errExternalTaskWithdrawn() error {
	return fmt.Errorf("%w: no such external task", apierr.ErrNotFound)
}

// withdrawnBecauseStepEnded withdraws the work parked on a step that is no
// longer waiting for it, and reports whether it did.
//
// A step that ends takes its parked work with it (Engine.endActivity), so a
// report normally finds either a step that is waiting or no task at all. The
// work that is found on a step that has ended was left there by a release
// before migration 31, which ended a step early without withdrawing anything
// and kept a token on it for every run. A report on it used to be refused by
// the engine; the refusal rolled back, the lock ran out, and the work was
// offered and refused again for as long as the instance existed.
//
// Whether the step is waiting is ProcessInstance.WaitsFor, the question the
// engine asks. All of the step's parked work goes, with one line on the
// instance's trail, as it would have when the step ended.
//
// A step the definition no longer describes is left to the engine, as before.
func (s *externalTaskService) withdrawnBecauseStepEnded(ctx context.Context, instance *entities.ProcessInstance, def *entities.ProcessDefinition, nodeID string) (bool, error) {
	node := def.FindNode(nodeID)
	if node == nil || instance.WaitsFor(node, "") {
		return false, nil
	}
	log.Info().
		Str("instance_id", instance.ID.String()).
		Str("node_id", nodeID).
		Msg("A worker reported on work for a step that had already ended; the step's parked work was withdrawn")
	return true, s.engine.WithdrawParkedWork(ctx, instance, node)
}

// HandleFailure records a worker's failure to do an external task.
//
// retries is how many tries the worker says are left. With some left, the
// task waits out retryTimeout (milliseconds) before it is offered again: the
// wait was stored and never read, so the task went straight back to be failed
// by whatever had just failed it. With none left, it raises an incident and is
// no longer offered — it used to be logged and nothing else, so nobody was
// told and the instance waited at the step with nothing to investigate.
// Resolving the incident offers it again.
func (s *externalTaskService) HandleFailure(ctx context.Context, taskID uuid.UUID, workerID string, errorMessage string, errorDetails string, retries int, retryTimeout int64) error {
	withdrawn := false
	err := s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		m, err := s.repo.ExternalTask().Get(txCtx, taskID)
		if err != nil {
			return err
		}

		// The instance first and the task's row second, for every way out of
		// here — the order Complete takes them in, and the engine when a step
		// ends and it withdraws the work still parked for it.
		//
		// This used to write the row and then, with no tries left, raise an
		// incident. An incident refers to its instance, so raising one waits
		// for whoever holds the instance — and the completion that ends the
		// step holds it while it waits to withdraw this row. Each held what the
		// other wanted. Short of that, the row read here could be withdrawn
		// before it was written, and the worker was told its failure had failed.
		//
		// Read again under the lock, so a task withdrawn in the meantime is
		// answered as what it is: no such task.
		instance, err := s.engine.GetInstanceForUpdate(txCtx, uuid.UUID(m.ProcessInstanceID))
		if err != nil {
			return err
		}
		if m, err = s.repo.ExternalTask().Get(txCtx, taskID); err != nil {
			return err
		}
		if err := refuseWorkerWithoutLock(m.WorkerID, workerID); err != nil {
			return err
		}

		// A failure at work for a step that has ended is nobody's to retry or
		// to resolve: the work is withdrawn, as a completion of it would be.
		if instance.Definition != nil {
			def, err := s.engine.GetProcessDefinition(txCtx, instance.Definition.ID)
			if err != nil {
				return err
			}
			if withdrawn, err = s.withdrawnBecauseStepEnded(txCtx, &instance, def, m.NodeID); err != nil || withdrawn {
				return err
			}
		}

		m.ErrorMessage = errorMessage
		m.ErrorDetails = errorDetails
		m.Retries = max(retries, 0)
		m.RetryTimeout = retryTimeout
		m.WorkerID = ""
		m.LockExpiration = nil
		if retries > 0 && retryTimeout > 0 {
			// Held, with nobody holding it, until the wait is over.
			until := time.Now().Add(time.Duration(retryTimeout) * time.Millisecond)
			m.LockExpiration = &until
		}
		if err := s.repo.ExternalTask().Update(txCtx, m); err != nil {
			return err
		}
		if retries > 0 {
			return nil
		}

		log.Error().
			Str("task_id", taskID.String()).
			Str("error", errorMessage).
			Msg("An external task failed with no retries left; an incident was raised")
		incident := entities.Incident{
			ID:        uuid.New(),
			Instance:  &entities.ProcessInstance{ID: uuid.UUID(m.ProcessInstanceID)},
			Node:      &entities.Node{ID: m.NodeID},
			Error:     redaction.RedactText(strings.TrimSpace(errorMessage + "\n" + errorDetails)),
			Status:    entities.IncidentOpen,
			CreatedAt: time.Now(),
		}
		if instance.Definition != nil {
			incident.Definition = &entities.ProcessDefinition{ID: instance.Definition.ID}
		}
		_, err = s.repo.Incident().Create(txCtx, adapters.IncidentModelAdapter{Incident: incident}.ToModel())
		return err
	})
	if err != nil {
		return err
	}
	if withdrawn {
		return errExternalTaskWithdrawn()
	}
	return nil
}

// refuseWorkerWithoutLock refuses a report from a worker the task is not
// locked by.
//
// A refusal, not a failure — the same 400 extending a lock one does not hold
// gets: sending the report again will not help, and the worker should stop and
// fetch. It names no task; the worker knows which one it asked about.
func refuseWorkerWithoutLock(heldBy, workerID string) error {
	if heldBy != workerID {
		return apierr.Invalidf("worker %q does not hold the lock on this task: it is another worker's, or "+
			"nobody's — fetch it again", workerID)
	}
	return nil
}

func (s *externalTaskService) Create(ctx context.Context, task *entities.ExternalTask) error {
	if task.ID == uuid.Nil {
		var err error
		task.ID, err = uuid.NewV7()
		if err != nil {
			return err
		}
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}
	model := adapters.ExternalTaskModelAdapter{ExternalTask: *task}.ToModel()
	return s.repo.ExternalTask().Create(ctx, &model)
}
