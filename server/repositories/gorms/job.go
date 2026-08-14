package gorms

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/contracts"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"gorm.io/gorm"
)

type jobRepository struct {
	db *gorm.DB
}

func NewJobRepository(db *gorm.DB) contracts.JobRepository {
	return &jobRepository{db: db}
}

func (r *jobRepository) Create(ctx context.Context, job models.JobModel) (uuid.UUID, error) {
	if job.ID == models.NilUUID {
		id, err := uuid.NewV7()
		if err != nil {
			return uuid.Nil, fmt.Errorf("generate job id: %w", err)
		}
		job.ID = models.UUID(id)
	}
	err := GetTx(ctx, r.db).Create(&job).Error
	return uuid.UUID(job.ID), err
}

func (r *jobRepository) Get(ctx context.Context, id uuid.UUID) (models.JobModel, error) {
	var m models.JobModel
	err := GetTx(ctx, r.db).First(&m, "id = ?", id).Error
	return m, err
}

func (r *jobRepository) Update(ctx context.Context, job models.JobModel) error {
	return GetTx(ctx, r.db).Save(&job).Error
}

func (r *jobRepository) GetPending(ctx context.Context, limit int) ([]models.JobModel, error) {
	var modelsList []models.JobModel
	now := time.Now()
	err := GetTx(ctx, r.db).
		Where("status = ? AND next_run_at <= ? AND (lock_expires IS NULL OR lock_expires < ?)", models.JobPending, now, now).
		Limit(limit).
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	return modelsList, nil
}

func (r *jobRepository) ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.JobModel, error) {
	var modelsList []models.JobModel
	err := GetTx(ctx, r.db).Where("instance_id = ?", instanceID).Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	return modelsList, nil
}

func (r *jobRepository) Lock(ctx context.Context, id uuid.UUID, lockDuration time.Duration, workerID string) (bool, error) {
	now := time.Now()
	expires := now.Add(lockDuration)
	result := GetTx(ctx, r.db).Model(&models.JobModel{}).
		Where("id = ? AND (status = ? OR lock_expires < ?)", id, models.JobPending, now).
		Updates(map[string]any{
			"status":       models.JobRunning,
			"locked_by":    workerID,
			"lock_expires": expires,
		})

	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
