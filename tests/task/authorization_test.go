package task_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	observerimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// These tests assert the DENIAL path. They exist because two authorization
// checks previously opened on an absent field:
//
//   - ClaimTask seeded its candidate check with `len(CandidateUsers) == 0`, so a
//     task restricted only by group was claimable by anyone, and CandidateGroups
//     was never consulted at all.
//   - CompleteTask guarded with `Assignee != nil && ...`, so an unassigned task
//     skipped authorization entirely — and tasks routed by candidate group are
//     created unassigned.

func newTaskService(t *testing.T) (repositories.Repository, servicecontracts.TaskService) {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	engine := serviceimpl.NewExecutionEngine(repo, observerimpl.NewEventDispatcher())
	svc := serviceimpl.NewTaskService(repo, engine, serviceimpl.NewAuditWriter(repo.Audit()))
	return repo, svc
}

func seedUserInGroup(t *testing.T, repo repositories.Repository, username, groupName string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	userID := uuid.Must(uuid.NewV7())
	if err := repo.User().Create(ctx, models.UserModel{
		Base:     models.Base{ID: models.UUID(userID)},
		Username: username,
		FullName: username,
	}, "hash"); err != nil {
		t.Fatalf("seed user %s: %v", username, err)
	}

	groupID := uuid.Must(uuid.NewV7())
	if groupName != "" {
		if err := repo.Group().Create(ctx, models.GroupModel{
			Base: models.Base{ID: models.UUID(groupID)},
			Name: groupName,
		}); err != nil {
			t.Fatalf("seed group %s: %v", groupName, err)
		}
		if err := repo.Group().AddMembership(ctx, userID, groupID); err != nil {
			t.Fatalf("add %s to %s: %v", username, groupName, err)
		}
	}
	return userID, groupID
}

func seedTask(t *testing.T, repo repositories.Repository, task entities.Task) uuid.UUID {
	t.Helper()
	if task.ID == uuid.Nil {
		task.ID = uuid.Must(uuid.NewV7())
	}
	if err := repo.Task().Create(t.Context(), adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	return task.ID
}

func TestClaimTask_DeniesNonMemberWhenRestrictedByGroupOnly(t *testing.T) {
	repo, svc := newTaskService(t)
	_, financeID := seedUserInGroup(t, repo, "alice", "finance")
	seedUserInGroup(t, repo, "mallory", "")

	taskID := seedTask(t, repo, entities.Task{
		Name:   "Approve invoice",
		Status: entities.TaskUnclaimed,
		Node:   &entities.Node{ID: "approve"},
		// Restricted by group only — CandidateUsers is empty, which is exactly
		// the shape that used to grant access to everyone.
		CandidateGroups: []*entities.Group{{ID: financeID, Name: "finance"}},
	})

	err := svc.ClaimTask(t.Context(), taskID, "mallory")
	if !errors.Is(err, serviceimpl.ErrTaskForbidden) {
		t.Fatalf("non-member claimed a group-restricted task: got %v, want ErrTaskForbidden", err)
	}
}

func TestClaimTask_AllowsGroupMember(t *testing.T) {
	repo, svc := newTaskService(t)
	_, financeID := seedUserInGroup(t, repo, "alice", "finance")

	taskID := seedTask(t, repo, entities.Task{
		Name:            "Approve invoice",
		Status:          entities.TaskUnclaimed,
		Node:            &entities.Node{ID: "approve"},
		CandidateGroups: []*entities.Group{{ID: financeID, Name: "finance"}},
	})

	if err := svc.ClaimTask(t.Context(), taskID, "alice"); err != nil {
		t.Fatalf("group member could not claim their own task: %v", err)
	}
}

func TestCompleteTask_DeniesNonCandidateOnUnassignedTask(t *testing.T) {
	repo, svc := newTaskService(t)
	_, financeID := seedUserInGroup(t, repo, "alice", "finance")
	seedUserInGroup(t, repo, "mallory", "")

	taskID := seedTask(t, repo, entities.Task{
		Name:   "Approve invoice",
		Status: entities.TaskUnclaimed,
		Node:   &entities.Node{ID: "approve"},
		// Assignee deliberately nil: this is how CreateTaskForNode leaves every
		// task routed by candidate group.
		Assignee:        nil,
		CandidateGroups: []*entities.Group{{ID: financeID, Name: "finance"}},
	})

	err := svc.CompleteTask(t.Context(), taskID, "mallory", map[string]any{"approved": true})
	if !errors.Is(err, serviceimpl.ErrTaskForbidden) {
		t.Fatalf("outsider completed an unassigned restricted task: got %v, want ErrTaskForbidden", err)
	}
}

func TestCompleteTask_DeniesNonAssignee(t *testing.T) {
	repo, svc := newTaskService(t)
	seedUserInGroup(t, repo, "alice", "")
	seedUserInGroup(t, repo, "mallory", "")

	taskID := seedTask(t, repo, entities.Task{
		Name:     "Approve invoice",
		Status:   entities.TaskClaimed,
		Node:     &entities.Node{ID: "approve"},
		Assignee: &entities.User{Username: "alice"},
	})

	err := svc.CompleteTask(t.Context(), taskID, "mallory", nil)
	if !errors.Is(err, serviceimpl.ErrTaskForbidden) {
		t.Fatalf("non-assignee completed an assigned task: got %v, want ErrTaskForbidden", err)
	}
}
