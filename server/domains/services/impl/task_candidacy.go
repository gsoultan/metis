package impl

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// inCandidateGroups reports whether any of groups is one the task is offered
// to. A definition names a candidate group by its name or by its id, so both
// are matched.
func inCandidateGroups(task entities.Task, groups []models.GroupModel) bool {
	memberOf := make(map[string]struct{}, 2*len(groups))
	for _, g := range groups {
		memberOf[g.Name] = struct{}{}
		memberOf[g.ID.String()] = struct{}{}
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

// offeredTo reports whether the task is offered to account: it is one of the
// task's candidate users, or in one of its candidate groups. Membership is
// read from the database, as it is when somebody claims.
func (s *taskService) offeredTo(ctx context.Context, task entities.Task, account models.UserModel) (bool, error) {
	for _, u := range task.CandidateUsers {
		if u != nil && u.Username == account.Username {
			return true, nil
		}
	}
	if len(task.CandidateGroups) == 0 {
		return false, nil
	}
	groups, err := s.repo.Group().ListUserGroups(ctx, uuid.UUID(account.ID))
	if err != nil {
		return false, fmt.Errorf("resolve group membership for %s: %w", account.Username, err)
	}
	return inCandidateGroups(task, groups), nil
}
