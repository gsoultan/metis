package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/clamp"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/processdefinition"
	"github.com/gsoultan/metis/server/repositories/store/retentionperiod"
)

type retentionPeriodRepository struct{ conn }

// NewRetentionPeriodRepository returns the store of the retention periods
// processes are given.
func NewRetentionPeriodRepository(c *db.Conn) contracts.RetentionPeriodRepository {
	return &retentionPeriodRepository{conn{conn: c}}
}

// retentionPeriodRowsRead bounds a project's rows read to work out its
// current periods. Each change is a row; a project whose periods were changed
// this often is answered from its newest changes, and says so.
const retentionPeriodRowsRead = 10_000

// errPeriodInAnEnvironment refuses a period set through an environment's
// port: the finished-instance sweep never reaches an environment's database,
// so the period would keep and remove nothing there.
var errPeriodInAnEnvironment = apierr.Invalidf("instances in an environment's own database are never removed by the retention sweep, so a period set there would decide nothing")

// Set records a change to a process's period.
//
// It waits for a sweep batch that is reading the periods (ForgetFinished),
// so a batch decides each instance by the periods as they were when it began,
// and the next by the change.
func (r *retentionPeriodRepository) Set(ctx context.Context, period entities.RetentionPeriod) (entities.RetentionPeriod, error) {
	if _, bound := db.EnvironmentFrom(ctx); bound {
		return entities.RetentionPeriod{}, errPeriodInAnEnvironment
	}
	if period.Project == nil || period.ProcessKey == "" {
		return entities.RetentionPeriod{}, errors.New("a retention period names a project and a process")
	}
	if err := r.requireProjectInTenant(ctx, period.Project.ID); err != nil {
		return entities.RetentionPeriod{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.RetentionPeriod{}, err
	}
	// A version that was deleted is not read, so a process all of whose
	// versions were deleted cannot be given a period: there is nothing left
	// to run under one.
	known, err := processdefinition.New().
		Where(processdefinition.ProjectID.Eq(period.Project.ID), processdefinition.Key.Eq(period.ProcessKey)).
		Exists(ctx, ex)
	if err != nil {
		return entities.RetentionPeriod{}, fmt.Errorf("could not check the process: %w", err)
	}
	if !known {
		return entities.RetentionPeriod{}, fmt.Errorf("%w: this project has no process with that key", apierr.ErrNotFound)
	}
	ins, err := stagePeriod(period)
	if err != nil {
		return entities.RetentionPeriod{}, err
	}
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		return entities.RetentionPeriod{}, fmt.Errorf("could not record the retention period: %w", err)
	}
	return periodFrom(row), nil
}

// Current reads a project's rows newest first and keeps the first of each
// process.
func (r *retentionPeriodRepository) Current(ctx context.Context, projectID uuid.UUID) ([]entities.RetentionPeriod, error) {
	rows, err := r.read(ctx, projectID, retentionperiod.New().Where(retentionperiod.ProjectID.Eq(projectID)), retentionPeriodRowsRead)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var current []entities.RetentionPeriod
	for _, row := range rows {
		if seen[row.ProcessKey] {
			continue
		}
		seen[row.ProcessKey] = true
		current = append(current, periodFrom(row))
	}
	return current, nil
}

// History lists one process's changes, newest first.
func (r *retentionPeriodRepository) History(ctx context.Context, projectID uuid.UUID, processKey string, limit int64) ([]entities.RetentionPeriod, error) {
	rows, err := r.read(ctx, projectID, retentionperiod.New().
		Where(retentionperiod.ProjectID.Eq(projectID), retentionperiod.ProcessKey.Eq(processKey)), limit)
	if err != nil {
		return nil, err
	}
	history := make([]entities.RetentionPeriod, 0, len(rows))
	for _, row := range rows {
		history = append(history, periodFrom(row))
	}
	return history, nil
}

func (r *retentionPeriodRepository) read(ctx context.Context, projectID uuid.UUID, q retentionperiod.Query, limit int64) ([]retentionperiod.Row, error) {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := q.Order(retentionperiod.CreatedAt.Desc(), retentionperiod.ID.Desc()).Limit(limit).All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the retention periods: %w", err)
	}
	return rows, nil
}

// stagePeriod sets every column of a new row. Its id is a v7: rows written in
// one transaction share a creation time, and the newest is told by id. Days
// outside the bounds the service enforces are refused here too, so the column
// never holds a period no caller could have asked for.
func stagePeriod(period entities.RetentionPeriod) (retentionperiod.Ins, error) {
	id := period.ID
	if id == uuid.Nil {
		id = uuid.Must(uuid.NewV7())
	}
	ins := retentionperiod.Create()
	ins.SetID(id)
	ins.SetProjectID(period.Project.ID)
	ins.SetProcessKey(period.ProcessKey)
	if period.Days != nil {
		days := *period.Days
		if days < 1 || days > entities.MaxRetentionPeriodDays {
			return ins, apierr.Invalidf("a retention period is from 1 to %d days", entities.MaxRetentionPeriodDays)
		}
		ins.SetDays(clamp.Int32(days))
	}
	ins.SetReason(period.Reason)
	ins.SetSetBy(period.SetBy)
	ins.SetSetByID(period.SetByID)
	return ins, nil
}

func periodFrom(row retentionperiod.Row) entities.RetentionPeriod {
	period := entities.RetentionPeriod{
		ID:         row.ID,
		CreatedAt:  row.CreatedAt,
		Project:    &entities.Project{ID: row.ProjectID},
		ProcessKey: row.ProcessKey,
		Reason:     row.Reason,
		SetBy:      row.SetBy,
		SetByID:    row.SetByID,
	}
	if row.Days.Valid {
		days := int(row.Days.V)
		period.Days = &days
	}
	return period
}
