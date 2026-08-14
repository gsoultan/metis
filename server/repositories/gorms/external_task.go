package gorms

import (
	"context"
	"fmt"
	"github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type externalTaskRepository struct {
	db *gorm.DB
}

func NewExternalTaskRepository(db *gorm.DB) contracts.ExternalTaskRepository {
	return &externalTaskRepository{db: db}
}

func (r *externalTaskRepository) Create(ctx context.Context, model *models.ExternalTaskModel) error {
	return ResolveDB(r.db).WithContext(ctx).Create(model).Error
}

func (r *externalTaskRepository) Get(ctx context.Context, id uuid.UUID) (*models.ExternalTaskModel, error) {
	var model models.ExternalTaskModel
	if err := ResolveDB(r.db).WithContext(ctx).First(&model, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("external task not found: %w", err)
	}
	return &model, nil
}

func (r *externalTaskRepository) Update(ctx context.Context, task *models.ExternalTaskModel) error {
	return ResolveDB(r.db).WithContext(ctx).Save(task).Error
}

func (r *externalTaskRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return ResolveDB(r.db).WithContext(ctx).Delete(&models.ExternalTaskModel{}, "id = ?", id).Error
}

func (r *externalTaskRepository) FetchAndLock(ctx context.Context, topic string, workerID string, maxTasks int, lockDuration int64) ([]*models.ExternalTaskModel, error) {
	var modelsList []*models.ExternalTaskModel
	now := time.Now()

	err := ResolveDB(r.db).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Find available tasks: topic matches, AND (lock_expiration is null OR lock_expiration < now) AND retries >= 0
		err := tx.Set("gorm:query_option", "FOR UPDATE").
			Where("topic = ? AND (lock_expiration IS NULL OR lock_expiration < ?) AND retries >= 0", topic, now).
			Limit(maxTasks).
			Find(&modelsList).Error
		if err != nil {
			return err
		}

		if len(modelsList) == 0 {
			return nil
		}

		expiration := now.Add(time.Duration(lockDuration) * time.Millisecond)
		ids := make([]uuid.UUID, len(modelsList))
		for i := range modelsList {
			ids[i] = uuid.UUID(modelsList[i].ID)
			modelsList[i].WorkerID = workerID
			modelsList[i].LockExpiration = &expiration
		}

		return tx.Model(&models.ExternalTaskModel{}).
			Where("id IN ?", ids).
			Updates(map[string]any{
				"worker_id":       workerID,
				"lock_expiration": expiration,
			}).Error
	})

	if err != nil {
		return nil, err
	}

	return modelsList, nil
}

func (r *externalTaskRepository) ListByProcessInstance(ctx context.Context, instanceID uuid.UUID) ([]*models.ExternalTaskModel, error) {
	var modelsList []*models.ExternalTaskModel
	if err := ResolveDB(r.db).WithContext(ctx).Where("process_instance_id = ?", instanceID).Find(&modelsList).Error; err != nil {
		return nil, err
	}
	return modelsList, nil
}
