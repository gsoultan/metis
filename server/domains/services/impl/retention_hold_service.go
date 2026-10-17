package impl

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// retentionHoldService places, releases and reads the holds that keep
// instances out of the finished-instance sweep.
type retentionHoldService struct {
	repo  repositories.Repository
	audit contracts.AuditWriter
}

// NewRetentionHoldService returns the service that places and releases
// retention holds, writing each act to the instance's audit trail.
func NewRetentionHoldService(repo repositories.Repository, audit contracts.AuditWriter) contracts.RetentionHoldService {
	return &retentionHoldService{repo: repo, audit: audit}
}

// HoldInstance places a hold on an instance of the caller's organization.
//
// The hold and the trail entry that says who placed it and why are written
// together or not at all. An instance already held is refused, and the hold
// that stands is left as it is: who placed it, and why, is part of the
// record, and a second administrator's reason does not replace the first's.
func (s *retentionHoldService) HoldInstance(ctx context.Context, instanceID uuid.UUID, reason string) (entities.RetentionHold, error) {
	caller, err := requireHoldingAdministrator(ctx)
	if err != nil {
		return entities.RetentionHold{}, err
	}
	reason, err = holdReason(reason)
	if err != nil {
		return entities.RetentionHold{}, err
	}
	instance, err := s.repo.Process().Get(ctx, instanceID)
	if err != nil {
		return entities.RetentionHold{}, err
	}
	hold := entities.RetentionHold{
		Project:    &entities.Project{ID: uuid.UUID(instance.ProjectID)},
		Instance:   &entities.ProcessInstance{ID: instanceID},
		Reason:     reason,
		PlacedBy:   caller.Username,
		PlacedByID: caller.ID,
	}
	var placed entities.RetentionHold
	err = s.repo.UnitOfWork().Do(ctx, func(ctx context.Context) error {
		placed, err = s.repo.RetentionHold().Place(ctx, hold)
		if err != nil {
			return err
		}
		return s.record(ctx, EventRetentionHoldPlaced, placed, caller.Username, reason)
	})
	if errors.Is(err, repocontracts.ErrInstanceAlreadyHeld) {
		return entities.RetentionHold{}, apierr.Invalidf("this instance is already held; release that hold first to place another")
	}
	if err != nil {
		return entities.RetentionHold{}, err
	}
	return placed, nil
}

// ReleaseInstanceHold releases the hold on an instance of the caller's
// organization, with the reason written to the trail beside who released it.
func (s *retentionHoldService) ReleaseInstanceHold(ctx context.Context, instanceID uuid.UUID, reason string) (entities.RetentionHold, error) {
	caller, err := requireHoldingAdministrator(ctx)
	if err != nil {
		return entities.RetentionHold{}, err
	}
	reason, err = holdReason(reason)
	if err != nil {
		return entities.RetentionHold{}, err
	}
	var released entities.RetentionHold
	err = s.repo.UnitOfWork().Do(ctx, func(ctx context.Context) error {
		released, err = s.repo.RetentionHold().Release(ctx, instanceID)
		if err != nil {
			return err
		}
		return s.record(ctx, EventRetentionHoldReleased, released, caller.Username, reason)
	})
	if err != nil {
		return entities.RetentionHold{}, err
	}
	return released, nil
}

// record writes one act on a hold to its instance's trail.
func (s *retentionHoldService) record(ctx context.Context, event string, hold entities.RetentionHold, actor, reason string) error {
	return s.audit.RecordEvent(ctx, entities.AuditEntry{
		Project:  hold.Project,
		Instance: hold.Instance,
		Type:     event,
		Message:  reason,
		Data:     map[string]any{"actor": actor, "reason": reason, "placed_by": hold.PlacedBy},
	})
}

// requireHoldingAdministrator returns the account placing or releasing a
// hold, which has to administer the organization the request is for and
// carry an account id: the hold records who placed it.
func requireHoldingAdministrator(ctx context.Context) (entities.User, error) {
	caller := signedIn(ctx)
	if caller == nil || caller.Username == "" || !administers(*caller, entities.ActingOrganization(ctx)) {
		return entities.User{}, apierr.Forbiddenf("only an administrator of the instance's organization can place or release a retention hold")
	}
	if caller.ID == uuid.Nil {
		return entities.User{}, apierr.Forbiddenf("a retention hold records the account that placed or released it, and this request carries none")
	}
	return *caller, nil
}

// holdReason is the reason as it is kept: trimmed, and refused when there is
// none or it is longer than a ledger row's.
func holdReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	switch {
	case reason == "":
		return "", apierr.Invalidf("say why: a retention hold is placed and released with a reason, which the instance's audit trail keeps")
	case utf8.RuneCountInString(reason) > entities.MaxRetentionHoldReasonLength:
		return "", apierr.Invalidf("the reason is longer than %d characters", entities.MaxRetentionHoldReasonLength)
	}
	return reason, nil
}
