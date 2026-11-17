package pg

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/startschedule"
	"github.com/gsoultan/storm/runtime"
)

// maxScheduleErrorLength is the longest failure kept on a row: enough to say
// what went wrong, not a place to keep a stack.
const maxScheduleErrorLength = 2000

type startScheduleRepository struct{ conn }

// NewStartScheduleRepository returns the store of where each process's
// scheduled starts have got to.
func NewStartScheduleRepository(c *db.Conn) contracts.StartScheduleRepository {
	return &startScheduleRepository{conn{conn: c}}
}

// Look inserts the row, or moves an existing one's look to now.
//
// Two statements rather than one upsert, because the two cases write
// different columns: a new row sets its watermark, and an existing one must
// keep the watermark it has. An upsert that carried the watermark would move
// it forward on every deploy and drop the run that was due in between.
func (r *startScheduleRepository) Look(ctx context.Context, projectID uuid.UUID, processKey string) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	now := nowUTC()
	ins := startschedule.Create()
	ins.SetProjectID(projectID)
	ins.SetProcessKey(processKey)
	ins.SetCheckedUntil(now)
	ins.SetNextRunAt(now)
	ins.SetLastErrorNull()
	ins.SetPausedAtNull()
	ins.OnConflictProcessKeyProjectID().DoNothing()
	_, err = ins.Insert(ctx, ex)
	if err == nil {
		return nil
	}
	if !errors.Is(err, runtime.ErrConflict) {
		return fmt.Errorf("could not record the schedule of %s: %w", processKey, err)
	}
	var look startschedule.Mut
	look.SetNextRunAt(now)
	look.SetUpdatedAt(now)
	if _, err := startschedule.New().
		Where(startschedule.ProjectID.Eq(projectID), startschedule.ProcessKey.Eq(processKey)).
		UpdateAll(ctx, ex, look); err != nil {
		return fmt.Errorf("could not ask for the schedule of %s to be looked at: %w", processKey, err)
	}
	return nil
}

func (r *startScheduleRepository) Due(ctx context.Context, now time.Time, limit int) ([]entities.StartScheduleState, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := startschedule.New().
		Where(startschedule.NextRunAt.Lte(now.UTC())).
		Order(startschedule.NextRunAt.Asc(), startschedule.ID.Asc()).
		Limit(int64(limit)).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the schedules that are due: %w", err)
	}
	out := make([]entities.StartScheduleState, 0, len(rows))
	for _, row := range rows {
		out = append(out, startScheduleFrom(row))
	}
	return out, nil
}

// Take is SKIP LOCKED, so two replicas looking at once take different rows
// instead of one waiting on the other; and it asks again whether the look is
// due, so the replica that arrives second finds the row already moved on.
func (r *startScheduleRepository) Take(ctx context.Context, id uuid.UUID, now time.Time) (entities.StartScheduleState, bool, error) {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.StartScheduleState{}, false, err
	}
	row, found, err := startschedule.New().
		Where(startschedule.ID.Eq(id), startschedule.NextRunAt.Lte(now.UTC())).
		ForUpdateSkipLocked().
		One(ctx, ex)
	if err != nil {
		return entities.StartScheduleState{}, false, fmt.Errorf("could not take the schedule: %w", err)
	}
	if !found {
		return entities.StartScheduleState{}, false, nil
	}
	return startScheduleFrom(row), true, nil
}

func (r *startScheduleRepository) Settle(ctx context.Context, id uuid.UUID, checkedUntil time.Time, nextLookAt *time.Time) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	var settled startschedule.Mut
	settled.SetCheckedUntil(checkedUntil.UTC())
	if nextLookAt == nil {
		settled.SetNextRunAtNull()
	} else {
		settled.SetNextRunAt(nextLookAt.UTC())
	}
	settled.SetFailures(0)
	settled.SetLastErrorNull()
	settled.SetUpdatedAt(nowUTC())
	written, err := startschedule.New().Where(startschedule.ID.Eq(id)).UpdateAll(ctx, ex, settled)
	if err != nil {
		return fmt.Errorf("could not record the schedule: %w", err)
	}
	if written == 0 {
		return fmt.Errorf("%w: no such schedule", apierr.ErrNotFound)
	}
	return nil
}

func (r *startScheduleRepository) Fail(ctx context.Context, id uuid.UUID, now time.Time, cause string, retryAt time.Time) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if len(cause) > maxScheduleErrorLength {
		cause = cause[:maxScheduleErrorLength]
	}
	var failed startschedule.Mut
	failed.SetNextRunAt(retryAt.UTC())
	failed.IncFailures()
	failed.SetLastError(cause)
	failed.SetUpdatedAt(nowUTC())
	if _, err := startschedule.New().
		Where(startschedule.ID.Eq(id), startschedule.NextRunAt.Lte(now.UTC())).
		UpdateAll(ctx, ex, failed); err != nil {
		return fmt.Errorf("could not record the failed schedule: %w", err)
	}
	return nil
}

func (r *startScheduleRepository) Get(ctx context.Context, projectID uuid.UUID, processKey string) (entities.StartScheduleState, error) {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return entities.StartScheduleState{}, fmt.Errorf("%w: no schedule for %s", apierr.ErrNotFound, processKey)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.StartScheduleState{}, err
	}
	row, found, err := startschedule.New().
		Where(startschedule.ProjectID.Eq(projectID), startschedule.ProcessKey.Eq(processKey)).
		One(ctx, ex)
	if err != nil {
		return entities.StartScheduleState{}, fmt.Errorf("could not read the schedule of %s: %w", processKey, err)
	}
	if !found {
		return entities.StartScheduleState{}, fmt.Errorf("%w: no schedule for %s", apierr.ErrNotFound, processKey)
	}
	return startScheduleFrom(row), nil
}

func (r *startScheduleRepository) Hold(ctx context.Context, projectID uuid.UUID, processKey string) (entities.StartScheduleState, error) {
	if err := r.requireProjectInTenant(ctx, projectID); err != nil {
		return entities.StartScheduleState{}, fmt.Errorf("%w: no schedule for %s", apierr.ErrNotFound, processKey)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.StartScheduleState{}, err
	}
	row, found, err := startschedule.New().
		Where(startschedule.ProjectID.Eq(projectID), startschedule.ProcessKey.Eq(processKey)).
		ForUpdate().
		One(ctx, ex)
	if err != nil {
		return entities.StartScheduleState{}, fmt.Errorf("could not hold the schedule of %s: %w", processKey, err)
	}
	if !found {
		return entities.StartScheduleState{}, fmt.Errorf("%w: no schedule for %s", apierr.ErrNotFound, processKey)
	}
	return startScheduleFrom(row), nil
}

func (r *startScheduleRepository) Pause(ctx context.Context, id uuid.UUID, now time.Time) error {
	var paused startschedule.Mut
	paused.SetPausedAt(now.UTC())
	paused.SetUpdatedAt(nowUTC())
	return r.writeHeld(ctx, id, paused, "pause")
}

func (r *startScheduleRepository) Resume(ctx context.Context, id uuid.UUID, checkedUntil time.Time, now time.Time) error {
	var resumed startschedule.Mut
	resumed.SetPausedAtNull()
	resumed.SetCheckedUntil(checkedUntil.UTC())
	resumed.SetNextRunAt(now.UTC())
	resumed.SetUpdatedAt(nowUTC())
	return r.writeHeld(ctx, id, resumed, "resume")
}

// writeHeld writes to the one row the caller holds.
func (r *startScheduleRepository) writeHeld(ctx context.Context, id uuid.UUID, change startschedule.Mut, doing string) error {
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	written, err := startschedule.New().Where(startschedule.ID.Eq(id)).UpdateAll(ctx, ex, change)
	if err != nil {
		return fmt.Errorf("could not %s the schedule: %w", doing, err)
	}
	if written == 0 {
		return fmt.Errorf("%w: no such schedule", apierr.ErrNotFound)
	}
	return nil
}

func startScheduleFrom(row startschedule.Row) entities.StartScheduleState {
	state := entities.StartScheduleState{
		ID:           row.ID,
		ProjectID:    row.ProjectID,
		ProcessKey:   row.ProcessKey,
		CheckedUntil: row.CheckedUntil,
		Failures:     int(row.Failures),
	}
	if row.NextRunAt.Valid {
		at := row.NextRunAt.V
		state.NextLookAt = &at
	}
	if row.LastError.Valid {
		state.LastError = row.LastError.V
	}
	if row.PausedAt.Valid {
		at := row.PausedAt.V
		state.PausedAt = &at
	}
	return state
}
