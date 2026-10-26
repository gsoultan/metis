package impl

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// taskActor is who is claiming or completing a task, and how their claim to
// it is checked.
//
// Two populations act on tasks: Users, through the platform's routes, and
// People, through their own. A task names either by the same kind of string,
// so the rules — assignee, candidate users, candidate groups, separation of
// duties — are one set, written once. What differs is where a name's groups
// come from and how far the actor reaches, and that is what this carries.
type taskActor struct {
	// name is what a task names them by: its assignee, a candidate user.
	name string
	// project is, for a person, the one project whose tasks they may touch.
	// uuid.Nil for a User, whose reach is the tenant scope's.
	project uuid.UUID
	// groupKeys returns the names and ids of the groups they belong to, read
	// from the database and never from the request.
	groupKeys func(ctx context.Context) ([]string, error)
	// takesUnnamed reports whether they may take a task nobody was named for.
	takesUnnamed func(ctx context.Context) bool
}

// reaches refuses a task outside the actor's project as not found: to a
// person, another project's work does not exist.
func (a taskActor) reaches(task entities.Task) error {
	if a.project == uuid.Nil {
		return nil
	}
	if task.Project == nil || task.Project.ID != a.project {
		return fmt.Errorf("%w: no such task", apierr.ErrNotFound)
	}
	return nil
}

// accountActor is a User acting on a task through the platform's routes. Their
// groups are the platform groups their account is in.
func (s *taskService) accountActor(userID string) taskActor {
	return taskActor{
		name: userID,
		groupKeys: func(ctx context.Context) ([]string, error) {
			userModel, err := s.repo.User().GetByUsername(ctx, userID)
			if err != nil {
				return nil, fmt.Errorf("%w: cannot resolve caller %s: %w", ErrTaskForbidden, userID, err)
			}
			groups, err := s.repo.Group().ListUserGroups(ctx, uuid.UUID(userModel.ID))
			if err != nil {
				return nil, fmt.Errorf("resolve group membership for %s: %w", userID, err)
			}
			keys := make([]string, 0, 2*len(groups))
			for _, g := range groups {
				keys = append(keys, g.Name, g.ID.String())
			}
			return keys, nil
		},
		takesUnnamed: func(ctx context.Context) bool { return mayTakeUnnamedTask(ctx, userID) },
	}
}

// personActor is a person acting on a task through the People routes. Their
// groups are the People groups of their project, and they never take a task
// nobody was named for: that is an administrator's or an operator's, and a
// person is neither.
func (s *taskService) personActor(person entities.Person) taskActor {
	return taskActor{
		name:    person.Username,
		project: person.ProjectID,
		groupKeys: func(ctx context.Context) ([]string, error) {
			groups, err := s.repo.PeopleDirectory().GroupsOf(ctx, person.ID)
			if err != nil {
				return nil, fmt.Errorf("resolve group membership for %s: %w", person.Username, err)
			}
			keys := make([]string, 0, 2*len(groups))
			for _, g := range groups {
				keys = append(keys, g.Name, g.ID.String())
			}
			return keys, nil
		},
		takesUnnamed: func(context.Context) bool { return false },
	}
}

// inCandidateGroupKeys reports whether any of keys — group names and ids —
// is one the task is offered to.
func inCandidateGroupKeys(task entities.Task, keys []string) bool {
	memberOf := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		memberOf[k] = struct{}{}
	}
	for _, cg := range task.CandidateGroups {
		if cg == nil {
			continue
		}
		if _, ok := memberOf[cg.Name]; ok {
			return true
		}
		if _, ok := memberOf[cg.ID.String()]; ok {
			return true
		}
	}
	return false
}
