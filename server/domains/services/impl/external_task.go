package impl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/redaction"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceContracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
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
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		m, err := s.repo.ExternalTask().Get(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := refuseReport(m, workerID); err != nil {
			return err
		}

		// Then hold the instance, and ask for the task again.
		//
		// The instance first and the task's row second, which is the order the
		// engine takes them in when an ad-hoc sub-process finishes and it
		// withdraws the work still parked inside (Engine.endAdHocSteps). This
		// used to delete the row and then wait for the instance; the completion
		// that finished the sub-process held the instance and waited to
		// withdraw that row. Each held what the other wanted, the database
		// ended one of them as a deadlock, and a worker was told its report had
		// failed — or a person that their task could not be completed.
		//
		// Read again because the wait is where the task goes: withdrawn by the
		// completion that finished its sub-process, or completed by an earlier
		// report of the same work. Either way it is no longer there to report
		// on, and the worker is told so.
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
		if err := refuseReport(m, workerID); err != nil {
			return err
		}
		task := adapters.ExternalTaskEntityAdapter{Model: *m}.ToEntity()

		// 1. DeleteConnectorInstance external task
		if err := s.repo.ExternalTask().Delete(txCtx, taskID); err != nil {
			return err
		}

		// 2. The definition the instance is running.
		def, err := s.engine.GetProcessDefinition(txCtx, instance.Definition.ID)
		if err != nil {
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
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		m, err := s.repo.ExternalTask().Get(txCtx, taskID)
		if err != nil {
			return err
		}

		if m.WorkerID != workerID {
			return fmt.Errorf("task %s is locked by another worker", taskID)
		}

		// The instance first and the task's row second, for every way out of
		// here — the order Complete takes them in, and the engine when an
		// ad-hoc sub-process finishes and it withdraws the work still parked
		// inside.
		//
		// This used to write the row and then, with no tries left, raise an
		// incident. An incident refers to its instance, so raising one waits
		// for whoever holds the instance — and the completion that finishes the
		// sub-process holds it while it waits to withdraw this row. Each held
		// what the other wanted. Short of that, the row read here could be
		// withdrawn before it was written, and the worker was told its failure
		// had failed.
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
		if m.WorkerID != workerID {
			return fmt.Errorf("task %s is locked by another worker", taskID)
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
}

// refuseReport refuses a worker's report on a task it does not hold: the task
// is locked by another worker, or the lock has run out.
func refuseReport(m *models.ExternalTaskModel, workerID string) error {
	taskID := uuid.UUID(m.ID)
	if m.WorkerID != workerID {
		return fmt.Errorf("task %s is locked by another worker", taskID)
	}
	if m.LockExpiration != nil && m.LockExpiration.Before(time.Now()) {
		return fmt.Errorf("lock for task %s has expired", taskID)
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
