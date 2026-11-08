package impl

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// participantIn reads one of the project's people by id. Somebody in another
// project, or nobody, is not found: the id alone would otherwise let a caller
// who manages one project reach into another's directory.
func (s *peopleSwitchOverService) participantIn(ctx context.Context, projectID, id uuid.UUID) (entities.WorkflowUser, error) {
	if projectID == uuid.Nil {
		return entities.WorkflowUser{}, apierr.Invalidf("a project is required")
	}
	if id == uuid.Nil {
		return entities.WorkflowUser{}, apierr.Invalidf("a participant is required")
	}
	people, err := s.participants.ListByProject(ctx, projectID, 0)
	if err != nil {
		return entities.WorkflowUser{}, err
	}
	for _, p := range people {
		if p.ID == id {
			return p, nil
		}
	}
	return entities.WorkflowUser{}, fmt.Errorf("%w: no such participant in this project", apierr.ErrNotFound)
}

// HeldTasks lists the project's open tasks the person holds.
func (s *peopleSwitchOverService) HeldTasks(ctx context.Context, projectID, participantID uuid.UUID) ([]entities.HeldTask, error) {
	person, err := s.participantIn(ctx, projectID, participantID)
	if err != nil {
		return nil, err
	}
	return s.heldBy(ctx, projectID, person.Username)
}

func (s *peopleSwitchOverService) heldBy(ctx context.Context, projectID uuid.UUID, username string) ([]entities.HeldTask, error) {
	rows, err := s.repo.Task().ListOpenByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the project's open tasks: %w", err)
	}
	var held []entities.HeldTask
	for _, m := range rows {
		t := adapters.TaskEntityAdapter{Model: m}.ToEntity()
		if t.AssigneeUsername() != username {
			continue
		}
		h := entities.HeldTask{TaskID: t.ID, Name: t.Name, Since: t.CreatedAt}
		if t.Instance != nil {
			h.InstanceID = t.Instance.ID
		}
		held = append(held, h)
	}
	return held, nil
}

// SetParticipantActive deactivates or reactivates one of the project's people.
//
// Deactivating stops new work reaching them and their People sign-in, and
// keeps the record of what they did. The open tasks they hold are not left
// silently: the caller says what becomes of them, and the result names every
// one still with them, so nothing is stranded without somebody being told.
// Each task is moved as a hand-over by the caller, with their reason, so the
// task's history says who moved it and why; a move the hand-over rules refuse
// leaves that task with the person and says why.
//
// Reactivating moves nothing back: the work went somewhere on purpose.
func (s *peopleSwitchOverService) SetParticipantActive(ctx context.Context, projectID, participantID uuid.UUID, active bool, work entities.HeldWork, actor string) (entities.ActivationResult, error) {
	person, err := s.participantIn(ctx, projectID, participantID)
	if err != nil {
		return entities.ActivationResult{}, err
	}
	if !active {
		if err := checkHeldWork(work); err != nil {
			return entities.ActivationResult{}, err
		}
	}
	if person.Active != active {
		person.Active = active
		if _, err := s.participants.Upsert(ctx, person); err != nil {
			return entities.ActivationResult{}, fmt.Errorf("could not %s %s: %w", map[bool]string{true: "activate", false: "deactivate"}[active], person.Username, err)
		}
	}
	result := entities.ActivationResult{Participant: person}
	if active {
		return result, nil
	}

	held, err := s.heldBy(ctx, projectID, person.Username)
	if err != nil {
		return result, err
	}
	for _, h := range held {
		var moveErr error
		switch work.Do {
		case entities.HeldWorkHandOver:
			moveErr = s.tasks.AssignTask(ctx, h.TaskID, servicecontracts.HandOver{Actor: actor, Target: work.To, Reason: work.Reason})
		case entities.HeldWorkRelease:
			moveErr = s.tasks.UnclaimTask(ctx, h.TaskID, servicecontracts.HandOver{Actor: actor, Reason: work.Reason})
		default:
			result.Kept = append(result.Kept, entities.KeptTask{HeldTask: h})
			continue
		}
		if moveErr != nil {
			result.Kept = append(result.Kept, entities.KeptTask{HeldTask: h, Why: moveErr.Error()})
			continue
		}
		result.Moved++
	}
	return result, nil
}

// checkHeldWork refuses a plan for held work that could not be carried out,
// before anybody is deactivated.
func checkHeldWork(work entities.HeldWork) error {
	switch work.Do {
	case "", entities.HeldWorkKeep:
		return nil
	case entities.HeldWorkRelease, entities.HeldWorkHandOver:
	default:
		return apierr.Invalidf("what to do with their open tasks is keep, release or hand_over, not %q", work.Do)
	}
	if strings.TrimSpace(work.Reason) == "" {
		return apierr.Invalidf("moving somebody's open tasks needs a reason: it is kept on each task's history")
	}
	if len([]rune(work.Reason)) > servicecontracts.MaxHandOverReasonLength {
		return apierr.Invalidf("the reason is longer than %d characters", servicecontracts.MaxHandOverReasonLength)
	}
	if work.Do == entities.HeldWorkHandOver && strings.TrimSpace(work.To) == "" {
		return apierr.Invalidf("say who their open tasks go to")
	}
	return nil
}
