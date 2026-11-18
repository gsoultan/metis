package impl

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// GetStartSchedule says whether a process starts on a schedule, whether that
// is paused, and when its next run is due.
func (s *definitionService) GetStartSchedule(ctx context.Context, projectID uuid.UUID, key string) (entities.StartScheduleStatus, error) {
	return s.startScheduleStatus(ctx, projectID, key, time.Now().UTC())
}

// SetStartSchedulePaused pauses a process's scheduled starts, or resumes
// them, and says where that leaves it.
//
// The row is held for the read and the write, so a look in progress finishes
// first and the pause applies to everything after it. Asking for the state
// the schedule is already in changes nothing: a second resume must not move
// the watermark and drop a run that is owed.
func (s *definitionService) SetStartSchedulePaused(ctx context.Context, projectID uuid.UUID, key string, paused bool) (entities.StartScheduleStatus, error) {
	now := time.Now().UTC()
	status, err := s.startScheduleStatus(ctx, projectID, key, now)
	if err != nil {
		return entities.StartScheduleStatus{}, err
	}
	if paused && !status.Scheduled {
		return entities.StartScheduleStatus{}, apierr.Invalidf("%q does not start on a schedule, so there is nothing to pause", key)
	}
	err = s.repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		state, err := s.repo.StartSchedule().Hold(txCtx, projectID, key)
		if errors.Is(err, apierr.ErrNotFound) && !paused {
			// Never scheduled, so never paused.
			return nil
		}
		if err != nil {
			return err
		}
		switch {
		case paused && state.PausedAt == nil:
			return s.repo.StartSchedule().Pause(txCtx, state.ID, now)
		case !paused && state.PausedAt != nil:
			// Nothing is owed for the time it was paused, including a time
			// that passed while no worker was running to pass it over.
			until := state.CheckedUntil
			if now.After(until) {
				until = now
			}
			return s.repo.StartSchedule().Resume(txCtx, state.ID, until, now)
		}
		return nil
	})
	if err != nil {
		return entities.StartScheduleStatus{}, err
	}
	return s.startScheduleStatus(ctx, projectID, key, now)
}

func (s *definitionService) startScheduleStatus(ctx context.Context, projectID uuid.UUID, key string, now time.Time) (entities.StartScheduleStatus, error) {
	if key == "" {
		return entities.StartScheduleStatus{}, apierr.Invalidf("a process key is required")
	}
	live, err := s.repo.Definition().GetLiveByProjectKey(ctx, projectID, key)
	if err != nil {
		return entities.StartScheduleStatus{}, err
	}
	status := entities.StartScheduleStatus{ProcessKey: key}
	state, err := s.repo.StartSchedule().Get(ctx, projectID, key)
	switch {
	case err == nil:
		status.Paused = state.PausedAt != nil
		status.PausedAt = state.PausedAt
		status.Failures = state.Failures
		status.LastError = state.LastError
	case !errors.Is(err, apierr.ErrNotFound):
		return entities.StartScheduleStatus{}, err
	}
	next, scheduled, err := s.nextScheduledRun(ctx, projectID, key, live, now)
	if err != nil {
		return entities.StartScheduleStatus{}, err
	}
	status.Scheduled = scheduled
	if !status.Paused {
		status.NextRunAt = next
	}
	return status, nil
}

// nextScheduledRun finds the first scheduled time after now, and says whether
// any version that is live or arranged to be has a schedule at all.
//
// It walks the versions in the order they will be live: the live one until
// the first arranged cutover, then each arranged version until the one after
// it. The next run is the first scheduled time that falls while its own
// version is live, which is the rule the worker starts runs by.
func (s *definitionService) nextScheduledRun(ctx context.Context, projectID uuid.UUID, key string, live models.ProcessDefinitionModel, now time.Time) (*time.Time, bool, error) {
	timeline, err := s.repo.Definition().ListReleasesForKey(ctx, projectID, key)
	if err != nil {
		return nil, false, fmt.Errorf("read the release timeline: %w", err)
	}
	var arranged []models.ProcessDefinitionReleaseModel
	for _, entry := range timeline {
		if entry.ActivateAt.After(now) {
			arranged = append(arranged, entry)
		}
	}
	sort.Slice(arranged, func(i, j int) bool { return arranged[i].ActivateAt.Before(arranged[j].ActivateAt) })

	anyScheduled := false
	version, from := live, now
	for i := 0; ; i++ {
		var until *time.Time
		if i < len(arranged) {
			until = &arranged[i].ActivateAt
		}
		starts, err := scheduledStarts(adapters.DefinitionEntityAdapter{Model: version}.ToEntity())
		if err != nil {
			return nil, false, err
		}
		anyScheduled = anyScheduled || len(starts) > 0
		var first *time.Time
		for _, start := range starts {
			if coming, ok := start.schedule.Next(from); ok && (first == nil || coming.Before(*first)) {
				first = &coming
			}
		}
		if first != nil && (until == nil || first.Before(*until)) {
			at := first.UTC()
			return &at, true, nil
		}
		if until == nil {
			return nil, anyScheduled, nil
		}
		version, err = s.repo.Definition().GetByProjectKeyAndVersion(ctx, projectID, key, arranged[i].Version)
		if errors.Is(err, apierr.ErrNotFound) {
			// The arranged version was deleted. What runs then is decided at
			// the time, so nothing further ahead can be promised.
			return nil, anyScheduled, nil
		}
		if err != nil {
			return nil, false, fmt.Errorf("read version %d: %w", arranged[i].Version, err)
		}
		from = *until
	}
}
