package impl

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
)

// retentionPeriodHistoryLimit bounds the changes to one process's period a
// read returns, newest first.
const retentionPeriodHistoryLimit = 1000

// maxProcessKeyLength is the longest process key a definition can have.
const maxProcessKeyLength = 255

// retentionPeriodService sets and reads the periods processes are given.
type retentionPeriodService struct {
	repo repositories.Repository
}

// NewRetentionPeriodService returns the service that sets and reads
// processes' retention periods.
func NewRetentionPeriodService(repo repositories.Repository) contracts.RetentionPeriodService {
	return &retentionPeriodService{repo: repo}
}

// SetRetentionPeriod records a change to a process's period.
//
// The change is a row of its own, with who made it and why: a project has no
// audit trail of its own, and how long a process's records were kept, and on
// whose word, is a question an auditor asks. A shorter period is acted on by
// the next sweep, so it is said in the log as well.
func (s *retentionPeriodService) SetRetentionPeriod(ctx context.Context, projectID uuid.UUID, processKey string, days *int, reason string) (entities.RetentionPeriod, error) {
	caller := signedIn(ctx)
	if caller == nil || caller.Username == "" || !administers(*caller, entities.ActingOrganization(ctx)) {
		return entities.RetentionPeriod{}, apierr.Forbiddenf("only an administrator of the project's organization can set a process's retention period")
	}
	if caller.ID == uuid.Nil {
		return entities.RetentionPeriod{}, apierr.Forbiddenf("a retention period records the account that set it, and this request carries none")
	}
	processKey, err := periodProcessKey(processKey)
	if err != nil {
		return entities.RetentionPeriod{}, err
	}
	if days != nil && (*days < 1 || *days > entities.MaxRetentionPeriodDays) {
		return entities.RetentionPeriod{}, apierr.Invalidf("a retention period is a whole number of days from 1 to %d, or null to follow the installation's setting", entities.MaxRetentionPeriodDays)
	}
	reason, err = periodReason(reason)
	if err != nil {
		return entities.RetentionPeriod{}, err
	}
	set, err := s.repo.RetentionPeriod().Set(ctx, entities.RetentionPeriod{
		Project:    &entities.Project{ID: projectID},
		ProcessKey: processKey,
		Days:       days,
		Reason:     reason,
		SetBy:      caller.Username,
		SetByID:    caller.ID,
	})
	if err != nil {
		return entities.RetentionPeriod{}, err
	}
	event := log.Info().Str("project", projectID.String()).Str("process", processKey).Str("by", caller.Username)
	if days == nil {
		event.Msg("A process's retention period was returned to the installation's setting")
	} else {
		event.Int("days", *days).Msg("A process was given a retention period: its finished instances older than that are removed by the retention sweep")
	}
	return set, nil
}

func (s *retentionPeriodService) RetentionPeriods(ctx context.Context, projectID uuid.UUID) ([]entities.RetentionPeriod, error) {
	return s.repo.RetentionPeriod().Current(ctx, projectID)
}

func (s *retentionPeriodService) RetentionPeriodHistory(ctx context.Context, projectID uuid.UUID, processKey string) ([]entities.RetentionPeriod, error) {
	processKey, err := periodProcessKey(processKey)
	if err != nil {
		return nil, err
	}
	return s.repo.RetentionPeriod().History(ctx, projectID, processKey, retentionPeriodHistoryLimit)
}

func periodProcessKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || utf8.RuneCountInString(key) > maxProcessKeyLength {
		return "", apierr.Invalidf("name the process by its key, of at most %d characters", maxProcessKeyLength)
	}
	return key, nil
}

// periodReason is the reason as it is kept: trimmed, and refused when there
// is none or it is longer than a hold's.
func periodReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	switch {
	case reason == "":
		return "", apierr.Invalidf("say why: a retention period is set with a reason, which is kept with it")
	case utf8.RuneCountInString(reason) > entities.MaxRetentionHoldReasonLength:
		return "", apierr.Invalidf("the reason is longer than %d characters", entities.MaxRetentionHoldReasonLength)
	}
	return reason, nil
}
