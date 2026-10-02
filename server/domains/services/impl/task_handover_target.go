package impl

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/models"
)

// admitTarget decides whether a task may be handed to step.target, and reports
// whether doing so overrides who the task is offered to.
//
// Deny by default: the name has to be an account, in the organization the task
// belongs to, that separation of duties does not bar from the step and — when
// the step is offered to people or teams — is one of them.
func (s *taskService) admitTarget(ctx context.Context, step handOverStep) (bool, error) {
	if step.target == step.task.AssigneeUsername() {
		return false, apierr.Invalidf("%s already holds this task", step.target)
	}
	account, err := s.memberCalled(ctx, step.row, step.target)
	if err != nil {
		return false, err
	}
	other, conflict, err := s.conflictingStep(ctx, step.row, step.target)
	if err != nil {
		return false, err
	}
	if conflict {
		// Refused outright, for an administrator as for anybody (D9): the
		// person could never complete it, so the task would only be parked.
		return false, apierr.Invalidf("%s already did %q on this case, and the same person may not also do %q; hand it to somebody else",
			step.target, stepName(other), step.task.Name)
	}
	return s.admitCandidacy(ctx, step, account)
}

// memberCalled reads the account a task is being handed to.
//
// A name nobody has and a name somebody in another organization has are told
// the same thing. Accounts are looked up installation-wide, so saying which it
// was would let anybody learn another organization's usernames by asking.
func (s *taskService) memberCalled(ctx context.Context, row models.TaskModel, username string) (models.UserModel, error) {
	nobody := apierr.Invalidf("there is nobody called %q in this organization to hand the task to", username)
	account, err := s.repo.User().GetByUsername(ctx, username)
	if errors.Is(err, apierr.ErrNotFound) {
		return models.UserModel{}, nobody
	}
	if err != nil {
		return models.UserModel{}, fmt.Errorf("look up who the task is handed to: %w", err)
	}
	project, err := s.repo.Project().Get(ctx, uuid.UUID(row.ProjectID))
	if err != nil {
		return models.UserModel{}, fmt.Errorf("read the project the task belongs to: %w", err)
	}
	for _, organization := range account.Organizations {
		if organization.ID == project.OrganizationID {
			return account, nil
		}
	}
	return models.UserModel{}, nobody
}

// admitCandidacy holds a hand-over to the people and teams the task is offered
// to, when it names any. Its holder may hand it only to one of them. An
// administrator may send it to somebody else, with a reason, and that is an
// override the trail records.
func (s *taskService) admitCandidacy(ctx context.Context, step handOverStep, account models.UserModel) (bool, error) {
	if len(step.task.CandidateUsers) == 0 && len(step.task.CandidateGroups) == 0 {
		return false, nil
	}
	offered, err := s.offeredTo(ctx, step.task, account)
	if err != nil || offered {
		return false, err
	}
	if !step.caller.administrator {
		return false, apierr.Invalidf("this task can be handed only to one of the people or teams it is offered to, and %s is not one of them; "+
			"an administrator can hand it to them with a reason", step.target)
	}
	if step.reason == "" {
		return false, apierr.Invalidf("%s is not one of the people this task is offered to; say why it goes to them anyway", step.target)
	}
	return true, nil
}

// stepName is what to call a step somebody already did, in a sentence.
func stepName(task models.TaskModel) string {
	if task.Name != "" {
		return task.Name
	}
	return "an earlier step"
}
