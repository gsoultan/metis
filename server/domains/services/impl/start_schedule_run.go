package impl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/redaction"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

const (
	// startScheduleBatch bounds how many processes one round looks at. A round
	// that comes back full is followed by another at the next poll, so this
	// caps a round's length, not how many schedules an installation may hold.
	startScheduleBatch = 50
	// startScheduleFirstRetry is how long a process whose look failed waits
	// before the next, doubling with each failure in a row up to
	// startScheduleLastRetry. The run it owed is still owed: the watermark
	// does not move until a look finishes.
	startScheduleFirstRetry = time.Minute
	startScheduleLastRetry  = time.Hour
)

// StartDueSchedules starts the runs that schedules owe as of now, and says
// how many it started.
//
// Each process is looked at in its own transaction, which holds the lock on
// its row, starts what is owed and moves the watermark past it. They commit
// together or not at all, so a scheduled time starts one run however many
// replicas are looking, and a look that fails leaves the run owed.
//
// One process failing does not stop the others: each is owed its own run.
func (e *Engine) StartDueSchedules(ctx context.Context, now time.Time) (int, error) {
	due, err := e.repo.StartSchedule().Due(ctx, now, startScheduleBatch)
	if err != nil {
		return 0, err
	}
	var errs []error
	started := 0
	for _, state := range due {
		n, err := e.lookAtSchedule(ctx, state.ID, now)
		started += n
		if err == nil {
			continue
		}
		err = fmt.Errorf("scheduled start of %s: %w", state.ProcessKey, err)
		errs = append(errs, err)
		retryAt := now.Add(startScheduleRetryAfter(state.Failures))
		cause := redaction.RedactText(err.Error())
		if failErr := e.repo.StartSchedule().Fail(ctx, state.ID, now, cause, retryAt); failErr != nil {
			errs = append(errs, fmt.Errorf("record the failed scheduled start of %s: %w", state.ProcessKey, failErr))
		}
	}
	return started, errors.Join(errs...)
}

// startScheduleRetryAfter is the wait before the look after failures in a row.
func startScheduleRetryAfter(failures int) time.Duration {
	wait := startScheduleFirstRetry
	for range failures {
		wait *= 2
		if wait >= startScheduleLastRetry {
			return startScheduleLastRetry
		}
	}
	return wait
}

// lookAtSchedule deals with one process: starts the runs its live version's
// schedules owe, and works out when it next needs looking at.
func (e *Engine) lookAtSchedule(ctx context.Context, id uuid.UUID, now time.Time) (int, error) {
	started := 0
	err := e.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		started = 0
		state, taken, err := e.repo.StartSchedule().Take(txCtx, id, now)
		if err != nil || !taken {
			return err
		}
		// Never backwards: a replica whose clock is behind the one that
		// looked last would otherwise reopen times already dealt with.
		until := state.CheckedUntil
		if now.After(until) {
			until = now
		}

		live, err := e.repo.Definition().GetLiveByProjectKey(txCtx, state.ProjectID, state.ProcessKey)
		if errors.Is(err, apierr.ErrNotFound) {
			// Every version is gone, so nothing is scheduled.
			return e.repo.StartSchedule().Settle(txCtx, state.ID, until, nil)
		}
		if err != nil {
			return fmt.Errorf("find the live version: %w", err)
		}
		liveSince, nextCutover, err := liveWindow(txCtx, e.repo.Definition(), state.ProjectID, state.ProcessKey, live, now)
		if err != nil {
			return err
		}
		// A version owes nothing for the time before it was live, whatever
		// its schedule says about that time.
		from := state.CheckedUntil
		if liveSince.After(from) {
			from = liveSince
		}

		def := adapters.DefinitionEntityAdapter{Model: live}.ToEntity()
		starts, err := scheduledStarts(def)
		if err != nil {
			return err
		}
		next := nextCutover
		for _, start := range starts {
			// One run however many times were missed: a process that was
			// down over a weekend of hourly starts catches up once. A paused
			// one starts nothing, and the watermark moving on below is what
			// passes the time over for good.
			if owed, ok := start.schedule.Next(from); ok && !owed.After(now) && state.PausedAt == nil {
				cmd := NewStartProcessCommand(e, state.ProjectID, def.Key, def.Version, nil, uuid.Nil, "")
				cmd.startNodeID = start.nodeID
				if err := cmd.Execute(txCtx); err != nil {
					return fmt.Errorf("start at %q for %s: %w", start.nodeID, owed.UTC().Format(time.RFC3339), err)
				}
				started++
			}
			if coming, ok := start.schedule.Next(until); ok && (next == nil || coming.Before(*next)) {
				next = &coming
			}
		}
		return e.repo.StartSchedule().Settle(txCtx, state.ID, until, next)
	})
	if err != nil {
		return 0, err
	}
	return started, nil
}

// scheduledStart is one start step that starts its process on a schedule.
type scheduledStart struct {
	nodeID   string
	schedule entities.StartSchedule
}

// scheduledStarts lists the start steps of a version that start it on a
// schedule. Only the process's own: a start step inside a subprocess begins
// that subprocess, not a run.
func scheduledStarts(def *entities.ProcessDefinition) ([]scheduledStart, error) {
	var starts []scheduledStart
	for _, node := range def.Nodes {
		if node.Type != entities.StartEvent || node.ParentID != "" {
			continue
		}
		schedule, scheduled, err := entities.StartScheduleOf(node)
		if err != nil {
			return nil, fmt.Errorf("read the schedule of %q: %w", node.ID, err)
		}
		if scheduled {
			starts = append(starts, scheduledStart{nodeID: node.ID, schedule: schedule})
		}
	}
	return starts, nil
}

// liveWindow says since when the live version has been live, and when the
// next arranged cutover replaces it, nil when none is arranged.
//
// A key with no timeline runs its newest version, live since it was deployed.
func liveWindow(ctx context.Context, defs repocontracts.DefinitionRepository, projectID uuid.UUID, key string, live models.ProcessDefinitionModel, now time.Time) (time.Time, *time.Time, error) {
	timeline, err := defs.ListReleasesForKey(ctx, projectID, key)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("read the release timeline: %w", err)
	}
	since := live.CreatedAt
	var next *time.Time
	// Newest first. The entries not in the future start with the live one,
	// and it has been live since the oldest of the unbroken run of entries
	// naming it: promoting the version that is already live starts nothing
	// over.
	stillLive := true
	for _, entry := range timeline {
		at := entry.ActivateAt
		if at.After(now) {
			if next == nil || at.Before(*next) {
				next = &at
			}
			continue
		}
		stillLive = stillLive && entry.Version == live.Version
		if stillLive {
			since = at
		}
	}
	return since, next, nil
}
