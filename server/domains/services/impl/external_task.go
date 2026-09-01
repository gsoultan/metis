package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
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
	return s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		m, err := s.repo.ExternalTask().Get(txCtx, taskID)
		if err != nil {
			return err
		}
		task := adapters.ExternalTaskEntityAdapter{Model: *m}.ToEntity()

		if task.WorkerID != workerID {
			return fmt.Errorf("task %s is locked by another worker", taskID)
		}

		if task.LockExpiration != nil && task.LockExpiration.Before(time.Now()) {
			return fmt.Errorf("lock for task %s has expired", taskID)
		}

		// 1. DeleteConnectorInstance external task
		if err := s.repo.ExternalTask().Delete(txCtx, taskID); err != nil {
			return err
		}

		// 2. Fetch instance and definition
		instance, err := s.engine.GetInstance(txCtx, task.ProcessInstance.ID)
		if err != nil {
			return err
		}

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

func (s *externalTaskService) HandleFailure(ctx context.Context, taskID uuid.UUID, workerID string, errorMessage string, errorDetails string, retries int, retryTimeout int64) error {
	m, err := s.repo.ExternalTask().Get(ctx, taskID)
	if err != nil {
		return err
	}

	if m.WorkerID != workerID {
		return fmt.Errorf("task %s is locked by another worker", taskID)
	}

	m.ErrorMessage = errorMessage
	m.ErrorDetails = errorDetails
	m.Retries = retries
	m.RetryTimeout = retryTimeout
	m.WorkerID = ""
	m.LockExpiration = nil

	if retries <= 0 {
		// Log incident?
		log.Error().
			Str("task_id", taskID.String()).
			Str("error", errorMessage).
			Msg("External task failed with no retries left")
	}

	return s.repo.ExternalTask().Update(ctx, m)
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
