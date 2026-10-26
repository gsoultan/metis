package impl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// personContext is where a person's task work runs: their project's
// organization as the tenant, so every read and write is scoped as a User's
// in that organization is, and the project checked on top of it by the actor.
//
// The organization is read from the project, never from the request. A
// person carries no tenant of their own; one taken from a header would let
// them choose whose tasks they are scoped to.
func (s *taskService) personContext(ctx context.Context, person entities.Person) (context.Context, error) {
	if person.ID == uuid.Nil || person.ProjectID == uuid.Nil || person.Username == "" {
		return nil, fmt.Errorf("%w: no person is signed in", apierr.ErrForbidden)
	}
	project, err := s.repo.Project().Get(entities.WithSystemContext(ctx), person.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("read the project of %s: %w", person.Username, err)
	}
	return entities.WithTenantContext(ctx, entities.TenantContext{
		TenantID: uuid.UUID(project.OrganizationID).String(),
	}), nil
}

func (s *taskService) ListPersonTasks(ctx context.Context, person entities.Person, page repocontracts.Pagination) (repocontracts.Page[entities.Task], error) {
	scoped, err := s.personContext(ctx, person)
	if err != nil {
		return repocontracts.Page[entities.Task]{}, err
	}
	actor := s.personActor(person)
	groups, err := actor.groupKeys(scoped)
	if err != nil {
		return repocontracts.Page[entities.Task]{}, err
	}
	result, err := s.repo.Task().ListOpenForPersonPaged(scoped, person.ProjectID, person.Username, groups, page)
	if err != nil {
		return repocontracts.Page[entities.Task]{}, err
	}
	tasks := make([]entities.Task, len(result.Items))
	for i, m := range result.Items {
		tasks[i] = adapters.TaskEntityAdapter{Model: m}.ToEntity()
	}
	return repocontracts.NewPage(tasks, result.Total, page), nil
}

// GetPersonTask answers a task that is theirs or offered to them, and "not
// found" for every other one — including one that exists in their project but
// was given to somebody else. What other people were asked to do is not
// theirs to read.
func (s *taskService) GetPersonTask(ctx context.Context, person entities.Person, id uuid.UUID) (entities.Task, error) {
	scoped, err := s.personContext(ctx, person)
	if err != nil {
		return entities.Task{}, err
	}
	m, err := s.repo.Task().Get(scoped, id)
	if err != nil {
		return entities.Task{}, err
	}
	task := adapters.TaskEntityAdapter{Model: m}.ToEntity()
	actor := s.personActor(person)
	if err := actor.reaches(task); err != nil {
		return entities.Task{}, err
	}
	if task.AssigneeUsername() == person.Username {
		return task, nil
	}
	if task.Status == entities.TaskUnclaimed && task.AssigneeUsername() == "" {
		if err := s.authorizeCandidateAs(scoped, task, actor); err == nil {
			return task, nil
		}
	}
	return entities.Task{}, fmt.Errorf("%w: no such task", apierr.ErrNotFound)
}

// ClaimPersonTask and CompletePersonTask first ask GetPersonTask's question:
// a task the person cannot see is not found, never "forbidden, it is bob's",
// which would tell them it exists and whose it is. The action then checks
// again under its own lock, so a task taken in between is still refused.
func (s *taskService) ClaimPersonTask(ctx context.Context, person entities.Person, id uuid.UUID) error {
	if _, err := s.GetPersonTask(ctx, person, id); err != nil {
		return err
	}
	scoped, err := s.personContext(ctx, person)
	if err != nil {
		return err
	}
	return s.claimAs(scoped, id, s.personActor(person))
}

func (s *taskService) CompletePersonTask(ctx context.Context, person entities.Person, id uuid.UUID, vars map[string]any) error {
	if _, err := s.GetPersonTask(ctx, person, id); err != nil {
		return err
	}
	scoped, err := s.personContext(ctx, person)
	if err != nil {
		return err
	}
	return s.completeAs(scoped, id, s.personActor(person), vars)
}

// ReleasePersonTask gives back a task the person took from a pool.
//
// Only their own, and only one that was offered to more than them: a task a
// model gave to them by name has nobody else to go to, and releasing it would
// leave it with nobody at all. Who may move work they do not hold is a User's
// question, on the platform's routes.
func (s *taskService) ReleasePersonTask(ctx context.Context, person entities.Person, id uuid.UUID) error {
	scoped, err := s.personContext(ctx, person)
	if err != nil {
		return err
	}
	actor := s.personActor(person)
	return s.repo.UnitOfWork().Do(scoped, func(txCtx context.Context) error {
		row, err := s.lockedTask(txCtx, id)
		if err != nil {
			return err
		}
		task := adapters.TaskEntityAdapter{Model: row}.ToEntity()
		if err := actor.reaches(task); err != nil {
			return err
		}
		if task.AssigneeUsername() != person.Username {
			return fmt.Errorf("%w: no such task", apierr.ErrNotFound)
		}
		if err := refuseUnreleasable(task); err != nil {
			return err
		}
		if len(task.CandidateUsers) == 0 && len(task.CandidateGroups) == 0 {
			return apierr.Invalidf("this task was given to you by name; there is nobody to release it to")
		}
		task.Status = entities.TaskUnclaimed
		task.Assignee = nil
		task.Owner, task.DelegationState = nil, ""
		if err := s.repo.Task().Update(txCtx, adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
			return fmt.Errorf("failed to update task: %w", err)
		}
		return s.announceHandOver(txCtx, entities.ProcessEvent{
			Type:      entities.EventTaskUpdated,
			Instance:  task.Instance,
			Project:   task.Project,
			Node:      namedNode(task),
			Timestamp: time.Now().Unix(),
			Variables: task.Variables,
		}, task, EventTaskUnclaimed, handOverRecord{
			actor:          person.Username,
			previousHolder: person.Username,
			byHolder:       true,
		})
	})
}

// PeopleTasksOf is the People side of the task service: the same rules, the
// same rows and locks, reached as a person.
func PeopleTasksOf(tasks servicecontracts.TaskService) servicecontracts.PeopleTaskService {
	if people, ok := tasks.(servicecontracts.PeopleTaskService); ok {
		return people
	}
	return NewUnavailablePeopleTaskService()
}

// errNoPeopleTasks is what a facade built without the People side answers.
var errNoPeopleTasks = errors.New("people cannot act on tasks: this server was wired without it")

type unavailablePeopleTasks struct{}

// NewUnavailablePeopleTaskService refuses every People task call.
func NewUnavailablePeopleTaskService() servicecontracts.PeopleTaskService {
	return unavailablePeopleTasks{}
}

func (unavailablePeopleTasks) ListPersonTasks(context.Context, entities.Person, repocontracts.Pagination) (repocontracts.Page[entities.Task], error) {
	return repocontracts.Page[entities.Task]{}, errNoPeopleTasks
}

func (unavailablePeopleTasks) GetPersonTask(context.Context, entities.Person, uuid.UUID) (entities.Task, error) {
	return entities.Task{}, errNoPeopleTasks
}

func (unavailablePeopleTasks) ClaimPersonTask(context.Context, entities.Person, uuid.UUID) error {
	return errNoPeopleTasks
}

func (unavailablePeopleTasks) ReleasePersonTask(context.Context, entities.Person, uuid.UUID) error {
	return errNoPeopleTasks
}

func (unavailablePeopleTasks) CompletePersonTask(context.Context, entities.Person, uuid.UUID, map[string]any) error {
	return errNoPeopleTasks
}
