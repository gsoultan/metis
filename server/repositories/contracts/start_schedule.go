package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// StartScheduleRepository keeps where each process's scheduled starts have
// got to, in the runtime the work is bound to.
//
// It is read and written by the engine's own worker, which spans every
// organization the way the job worker does, and by the definition repository
// when a process's timeline changes. Nothing a caller sends names a row here.
type StartScheduleRepository interface {
	// Look asks for a process to be looked at now, because which of its
	// versions is live, or will be, has just changed. A process seen for the
	// first time starts with its watermark at now, so it owes nothing for the
	// time before it existed.
	//
	// It writes with the caller's transaction, so the look is asked for if
	// and only if the change it follows is kept.
	Look(ctx context.Context, projectID uuid.UUID, processKey string) error
	// Due lists the processes whose look has come, the longest overdue first.
	Due(ctx context.Context, now time.Time, limit int) ([]entities.StartScheduleState, error)
	// Take locks one process's row for the caller's transaction, if its look
	// is still due and nobody else holds it. It says false when another
	// worker has it or has already dealt with it.
	Take(ctx context.Context, id uuid.UUID, now time.Time) (entities.StartScheduleState, bool, error)
	// Settle records a finished look: the watermark it reached and when the
	// process next needs looking at, nil for never. It clears the failures.
	Settle(ctx context.Context, id uuid.UUID, checkedUntil time.Time, nextLookAt *time.Time) error
	// Fail records a look as of now that could not be finished, and when to
	// try again. The watermark stays where it was, so the run that was owed
	// still is. It writes nothing when the look is no longer due: another
	// worker finished it, and its answer stands.
	Fail(ctx context.Context, id uuid.UUID, now time.Time, cause string, retryAt time.Time) error
	// Get reads one process's state, or ErrNotFound.
	Get(ctx context.Context, projectID uuid.UUID, processKey string) (entities.StartScheduleState, error)
	// Hold reads one process's state and locks its row for the caller's
	// transaction, waiting for a look in progress to finish. ErrNotFound when
	// the process has no row or is not the caller's to see.
	Hold(ctx context.Context, projectID uuid.UUID, processKey string) (entities.StartScheduleState, error)
	// Pause marks a held row paused as of now. The watermark and the next
	// look stay as they are: a paused process is still looked at, and starts
	// nothing.
	Pause(ctx context.Context, id uuid.UUID, now time.Time) error
	// Resume clears the pause on a held row, moves the watermark to
	// checkedUntil and asks for a look now. The caller passes a watermark no
	// earlier than now, so no run is started for a time that passed while
	// the schedule was paused.
	Resume(ctx context.Context, id uuid.UUID, checkedUntil time.Time, now time.Time) error
}
