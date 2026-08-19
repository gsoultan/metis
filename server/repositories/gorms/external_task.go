package gorms

import (
	"context"
	"fmt"
	"time"

	"github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"

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
	return GetTx(ctx, r.db).Create(model).Error
}

// tableExternalTasks is the SQL table behind ExternalTaskModel, needed by name
// so the tenant scope can build its JOIN.
const tableExternalTasks = "external_tasks"

// Get returns an external task by ID, scoped to the caller's tenant — an ID
// from another organization reads as not found.
func (r *externalTaskRepository) Get(ctx context.Context, id uuid.UUID) (*models.ExternalTaskModel, error) {
	var model models.ExternalTaskModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), tableExternalTasks)
	if err := db.First(&model, QualifiedByID(tableExternalTasks), id).Error; err != nil {
		return nil, fmt.Errorf("external task not found: %w", err)
	}
	return &model, nil
}

// Update saves an external task, refusing an ID outside the caller's tenant.
// This is the path a worker reports completion or failure on, so an unscoped
// write would let one organization's worker resolve another's work.
func (r *externalTaskRepository) Update(ctx context.Context, task *models.ExternalTaskModel) error {
	db := GetTx(ctx, r.db)
	if err := requireVisibleToTenant(ctx, db, tableExternalTasks, &models.ExternalTaskModel{}, uuid.UUID(task.ID)); err != nil {
		return err
	}
	return db.Save(task).Error
}

// Delete removes an external task, refusing an ID outside the caller's tenant.
func (r *externalTaskRepository) Delete(ctx context.Context, id uuid.UUID) error {
	db := GetTx(ctx, r.db)
	if err := requireVisibleToTenant(ctx, db, tableExternalTasks, &models.ExternalTaskModel{}, id); err != nil {
		return err
	}
	return db.Delete(&models.ExternalTaskModel{}, QualifiedByID(tableExternalTasks), id).Error
}

// FetchAndLock is the worker long-poll. Topic names are chosen per project and
// nothing stops two organizations picking the same one, so without a tenant
// scope a worker authenticated into one of them would be handed the other's
// work — the endpoint checks that the caller is authenticated, not who the task
// belongs to.
//
// The scope is a subquery rather than a JOIN because this SELECT takes
// FOR UPDATE: joining projects would put row locks on a second table on every
// poll, and serialise workers against each other tenant-wide.
func (r *externalTaskRepository) FetchAndLock(ctx context.Context, topic string, workerID string, maxTasks int, lockDuration int64) ([]*models.ExternalTaskModel, error) {
	var modelsList []*models.ExternalTaskModel
	now := time.Now()

	err := GetTx(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		// Find available tasks: topic matches, AND (lock_expiration is null OR lock_expiration < now) AND retries >= 0
		err := tenantScopeCondition(ctx, tx, tableExternalTasks).
			Set("gorm:query_option", "FOR UPDATE").
			Where("external_tasks.topic = ? AND (external_tasks.lock_expiration IS NULL OR external_tasks.lock_expiration < ?) AND external_tasks.retries >= 0", topic, now).
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

// ListByProcessInstance returns an instance's external tasks, scoped to the
// caller's tenant.
func (r *externalTaskRepository) ListByProcessInstance(ctx context.Context, instanceID uuid.UUID) ([]*models.ExternalTaskModel, error) {
	var modelsList []*models.ExternalTaskModel
	db := tenantScopeDB(ctx, GetTx(ctx, r.db), tableExternalTasks)
	if err := db.Where("external_tasks.process_instance_id = ?", instanceID).Find(&modelsList).Error; err != nil {
		return nil, err
	}
	return modelsList, nil
}
