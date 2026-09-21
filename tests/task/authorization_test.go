package task_test

import (
	"context"
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

// Returns the scoped context and the project as well, because a task is
// reachable only through the project that owns it and the organization that
// owns that. Threading them is what makes these tests resemble a request.
func newTaskService(t *testing.T) (repositories.Repository, servicecontracts.TaskService, context.Context, uuid.UUID) {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	engine := serviceimpl.NewExecutionEngine(repo, observerimpl.NewEventDispatcher())
	svc := serviceimpl.NewTaskService(repo, engine, serviceimpl.NewAuditWriter(repo.Audit()))

	ctx, orgID := testutils.ScopedContext(t, repo)
	projectID := uuid.Must(uuid.NewV7())
	if err := repo.Project().Create(entities.WithSystemContext(ctx), models.ProjectModel{
		Base:           models.Base{ID: models.UUID(projectID)},
		OrganizationID: models.UUID(orgID),
		Name:           "Test Project",
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return repo, svc, ctx, projectID
}

func seedUserInGroup(t *testing.T, repo repositories.Repository, ctx context.Context, orgID uuid.UUID, username, groupName string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	// ctx and orgID come from the caller. A group belongs to an organization,
	// and every repository query in production arrives with that organization
	// resolved from the caller's token. Seeding through a bare t.Context()
	// only worked while the scope failed open, so this seeded a group into no
	// organization at all and the tests asserted a path no request takes.
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
			Base:           models.Base{ID: models.UUID(groupID)},
			Name:           groupName,
			OrganizationID: models.UUID(orgID),
		}); err != nil {
			t.Fatalf("seed group %s: %v", groupName, err)
		}
		if err := repo.Group().AddMembership(ctx, userID, groupID); err != nil {
			t.Fatalf("add %s to %s: %v", username, groupName, err)
		}
	}
	return userID, groupID
}

func seedTask(t *testing.T, repo repositories.Repository, ctx context.Context, projectID uuid.UUID, task entities.Task) uuid.UUID {
	t.Helper()
	if task.ID == uuid.Nil {
		task.ID = uuid.Must(uuid.NewV7())
	}
	model := adapters.TaskModelAdapter{Task: task}.ToModel()
	// Without the project the task belongs to nothing, and a scoped read
	// cannot reach it — which is also true in production.
	model.ProjectID = models.UUID(projectID)
	if err := repo.Task().Create(ctx, model); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	return task.ID
}

func TestClaimTask_DeniesNonMemberWhenRestrictedByGroupOnly(t *testing.T) {
	repo, svc, ctx, projectID := newTaskService(t)
	_, financeID := seedUserInGroup(t, repo, ctx, testutils.OrgIDFrom(t, ctx), "alice", "finance")
	seedUserInGroup(t, repo, ctx, testutils.OrgIDFrom(t, ctx), "mallory", "")

	taskID := seedTask(t, repo, ctx, projectID, entities.Task{
		Name:   "Approve invoice",
		Status: entities.TaskUnclaimed,
		Node:   &entities.Node{ID: "approve"},
		// Restricted by group only — CandidateUsers is empty, which is exactly
		// the shape that used to grant access to everyone.
		CandidateGroups: []*entities.Group{{ID: financeID, Name: "finance"}},
	})

	err := svc.ClaimTask(ctx, taskID, "mallory")
	if !errors.Is(err, serviceimpl.ErrTaskForbidden) {
		t.Fatalf("non-member claimed a group-restricted task: got %v, want ErrTaskForbidden", err)
	}
}

func TestClaimTask_AllowsGroupMember(t *testing.T) {
	repo, svc, ctx, projectID := newTaskService(t)
	_, financeID := seedUserInGroup(t, repo, ctx, testutils.OrgIDFrom(t, ctx), "alice", "finance")

	taskID := seedTask(t, repo, ctx, projectID, entities.Task{
		Name:            "Approve invoice",
		Status:          entities.TaskUnclaimed,
		Node:            &entities.Node{ID: "approve"},
		CandidateGroups: []*entities.Group{{ID: financeID, Name: "finance"}},
	})

	if err := svc.ClaimTask(ctx, taskID, "alice"); err != nil {
		t.Fatalf("group member could not claim their own task: %v", err)
	}
}

func TestCompleteTask_DeniesNonCandidateOnUnassignedTask(t *testing.T) {
	repo, svc, ctx, projectID := newTaskService(t)
	_, financeID := seedUserInGroup(t, repo, ctx, testutils.OrgIDFrom(t, ctx), "alice", "finance")
	seedUserInGroup(t, repo, ctx, testutils.OrgIDFrom(t, ctx), "mallory", "")

	taskID := seedTask(t, repo, ctx, projectID, entities.Task{
		Name:   "Approve invoice",
		Status: entities.TaskUnclaimed,
		Node:   &entities.Node{ID: "approve"},
		// Assignee deliberately nil: this is how CreateTaskForNode leaves every
		// task routed by candidate group.
		Assignee:        nil,
		CandidateGroups: []*entities.Group{{ID: financeID, Name: "finance"}},
	})

	err := svc.CompleteTask(ctx, taskID, "mallory", map[string]any{"approved": true})
	if !errors.Is(err, serviceimpl.ErrTaskForbidden) {
		t.Fatalf("outsider completed an unassigned restricted task: got %v, want ErrTaskForbidden", err)
	}
}

func TestCompleteTask_DeniesNonAssignee(t *testing.T) {
	repo, svc, ctx, projectID := newTaskService(t)
	seedUserInGroup(t, repo, ctx, testutils.OrgIDFrom(t, ctx), "alice", "")
	seedUserInGroup(t, repo, ctx, testutils.OrgIDFrom(t, ctx), "mallory", "")

	taskID := seedTask(t, repo, ctx, projectID, entities.Task{
		Name:     "Approve invoice",
		Status:   entities.TaskClaimed,
		Node:     &entities.Node{ID: "approve"},
		Assignee: &entities.User{Username: "alice"},
	})

	err := svc.CompleteTask(ctx, taskID, "mallory", nil)
	if !errors.Is(err, serviceimpl.ErrTaskForbidden) {
		t.Fatalf("non-assignee completed an assigned task: got %v, want ErrTaskForbidden", err)
	}
}
