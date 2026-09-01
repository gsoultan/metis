package gorms

import (
	"context"
	"fmt"
	"time"

	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"gorm.io/gorm"
)

type gormBroadcastRepository struct {
	db *gorm.DB
}

// NewBroadcastRepository creates the SSE fan-out repository.
func NewBroadcastRepository(db *gorm.DB) contracts.BroadcastRepository {
	return &gormBroadcastRepository{db: db}
}

func (r *gormBroadcastRepository) Publish(ctx context.Context, origin, payload string) error {
	// Deliberately not GetTx: an event is published as a fact about something
	// that already happened, and joining the caller's transaction would mean a
	// rollback silently un-notifies browsers about work that did commit
	// earlier in the same handler. It would also hold the row invisible until
	// commit, which is exactly when it is least useful.
	event := models.BroadcastEventModel{
		Origin:    origin,
		Payload:   payload,
		CreatedAt: time.Now().UTC(),
	}
	if err := r.db.WithContext(ctx).Create(&event).Error; err != nil {
		return fmt.Errorf("could not publish a broadcast event: %w", err)
	}
	return nil
}

func (r *gormBroadcastRepository) Since(ctx context.Context, origin string, afterID int64, limit int) ([]models.BroadcastEventModel, error) {
	var events []models.BroadcastEventModel
	err := r.db.WithContext(ctx).
		Where("id > ? AND origin <> ?", afterID, origin).
		Order("id ASC").
		Limit(limit).
		Find(&events).Error
	if err != nil {
		return nil, fmt.Errorf("could not read broadcast events: %w", err)
	}
	return events, nil
}

func (r *gormBroadcastRepository) LatestID(ctx context.Context) (int64, error) {
	// COALESCE rather than reading into a nullable: an empty table is the
	// ordinary state of a fresh installation, not a condition to handle.
	var latest int64
	err := r.db.WithContext(ctx).
		Model(&models.BroadcastEventModel{}).
		Select("COALESCE(MAX(id), 0)").
		Scan(&latest).Error
	if err != nil {
		return 0, fmt.Errorf("could not read the latest broadcast id: %w", err)
	}
	return latest, nil
}

func (r *gormBroadcastRepository) Prune(ctx context.Context, olderThan time.Time) (int64, error) {
	result := r.db.WithContext(ctx).
		Where("created_at < ?", olderThan).
		Delete(&models.BroadcastEventModel{})
	if result.Error != nil {
		return 0, fmt.Errorf("could not prune broadcast events: %w", result.Error)
	}
	return result.RowsAffected, nil
}
