package gorms

import (
	"context"
	"fmt"
	"time"

	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type gormSharedCounterRepository struct {
	db *gorm.DB
}

// NewSharedCounterRepository creates the cross-replica counter store.
func NewSharedCounterRepository(db *gorm.DB) contracts.SharedCounterRepository {
	return &gormSharedCounterRepository{db: db}
}

func (r *gormSharedCounterRepository) Record(ctx context.Context, scope, key, replica string, windowStart time.Time, count int64) error {
	row := models.SharedCounterModel{
		Scope:       scope,
		Key:         key,
		Replica:     replica,
		WindowStart: windowStart.UTC(),
		Count:       count,
		UpdatedAt:   time.Now().UTC(),
	}

	// Upsert on the whole primary key. This is the one place the dialects could
	// have disagreed, and it is a plain "insert or replace my own row" because
	// the replica is part of the key — there is no other writer to conflict
	// with, so no conditional logic and no RETURNING.
	//
	// Deliberately not GetTx: a limiter's bookkeeping must not join, or fail
	// with, whatever transaction the request happens to be in.
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "scope"}, {Name: "counter_key"}, {Name: "replica"}, {Name: "window_start"}},
		DoUpdates: clause.AssignmentColumns([]string{"count", "updated_at"}),
	}).Create(&row).Error
	if err != nil {
		return fmt.Errorf("could not record a shared counter: %w", err)
	}
	return nil
}

func (r *gormSharedCounterRepository) Totals(ctx context.Context, scope string, keys []string, windowStart time.Time) (map[string]int64, error) {
	totals := make(map[string]int64, len(keys))
	if len(keys) == 0 {
		return totals, nil
	}

	var rows []struct {
		CounterKey string
		Total      int64
	}
	err := r.db.WithContext(ctx).
		Model(&models.SharedCounterModel{}).
		Select("counter_key, SUM(count) as total").
		Where("scope = ? AND window_start = ? AND counter_key IN ?", scope, windowStart.UTC(), keys).
		Group("counter_key").
		Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("could not read shared counters: %w", err)
	}

	for _, row := range rows {
		totals[row.CounterKey] = row.Total
	}
	return totals, nil
}

func (r *gormSharedCounterRepository) Prune(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.WithContext(ctx).
		Where("window_start < ?", before.UTC()).
		Delete(&models.SharedCounterModel{})
	if result.Error != nil {
		return 0, fmt.Errorf("could not prune shared counters: %w", result.Error)
	}
	return result.RowsAffected, nil
}
