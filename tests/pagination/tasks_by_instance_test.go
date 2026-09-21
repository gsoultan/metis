package pagination_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/endpoints/task"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/tests/testutils"
)

// "What is this run waiting on" had no answer over HTTP. The task listing took
// a project and nothing else, so a client holding an instance id had to page
// through every task in the project and match — which only worked while the one
// it wanted was still on a page it could reach.

func seedTasksForInstance(t *testing.T, repo repositories.Repository, ctx context.Context, projectID, instanceID uuid.UUID, n int) {
	t.Helper()
	for i := range n {
		task := entities.Task{
			ID:       uuid.Must(uuid.NewV7()),
			Project:  &entities.Project{ID: projectID},
			Instance: &entities.ProcessInstance{ID: instanceID},
			Name:     "Task",
			Status:   entities.TaskUnclaimed,
			Node:     &entities.Node{ID: "approve"},
		}
		if err := repo.Task().Create(ctx, adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
			t.Fatalf("seed task %d: %v", i, err)
		}
	}
}

func TestListByInstancePaged_WindowsAndCountsOneInstance(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))

	ctx, _, projectID := testutils.ScopedProject(t, repo)
	wanted := uuid.Must(uuid.NewV7())
	other := uuid.Must(uuid.NewV7())

	seedTasksForInstance(t, repo, ctx, projectID, wanted, 73)
	// A second instance in the same project: the filter has to be the instance,
	// not the project, or this test passes for the wrong reason.
	seedTasksForInstance(t, repo, ctx, projectID, other, 40)

	page, err := repo.Task().ListByInstancePaged(ctx, wanted, contracts.Pagination{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}

	if len(page.Items) != 25 {
		t.Fatalf("page 1 returned %d rows, want 25 — the window is not limited", len(page.Items))
	}
	// 73, not 113: the count describes the instance's tasks, not the project's.
	if page.Total != 73 {
		t.Fatalf("Total = %d, want 73 — the other instance's tasks are being counted", page.Total)
	}
	if !page.HasMore() {
		t.Error("HasMore() = false on page 1 of 3")
	}

	for _, item := range page.Items {
		if uuid.UUID(item.InstanceID) != wanted {
			t.Fatalf("a task from instance %s came back", uuid.UUID(item.InstanceID))
		}
	}
}

// An instance with no tasks is an ordinary answer — the run has not reached a
// human step — and must not be a failure or somebody else's rows.
func TestListByInstancePaged_UnknownInstanceIsEmpty(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))

	ctx, _, projectID := testutils.ScopedProject(t, repo)
	seedTasksForInstance(t, repo, ctx, projectID, uuid.Must(uuid.NewV7()), 5)

	page, err := repo.Task().ListByInstancePaged(ctx, uuid.Must(uuid.NewV7()), contracts.Pagination{Page: 1, PageSize: 25})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 0 || page.Total != 0 {
		t.Errorf("an unknown instance returned %d rows (total %d)", len(page.Items), page.Total)
	}
}

// A malformed instance id must be refused rather than ignored. Falling through
// to the project branch would answer a wider question than the one asked — the
// same failure the malformed-project-id test above pins for instances.
func TestListTasksRefusesAMalformedInstanceID(t *testing.T) {
	ep := task.MakeListTasksEndpoint(nil)

	got, err := ep(t.Context(), task.ListTasksRequest{
		InstanceID: "not-a-uuid",
		// A valid project alongside it: the instance branch has to be reached
		// and refuse, not silently hand over to the project listing.
		ProjectID: uuid.Must(uuid.NewV7()).String(),
	})
	if err != nil {
		t.Fatalf("the endpoint returned a transport error: %v", err)
	}

	res, ok := got.(task.ListTasksResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", got)
	}
	if res.Err == nil {
		t.Fatal("a malformed instance id was accepted; the listing would have widened to the whole project")
	}
	if !strings.Contains(res.Err.Error(), "not a valid identifier") {
		t.Errorf("the error does not explain what was wrong: %v", res.Err)
	}
	if len(res.Tasks) != 0 {
		t.Errorf("tasks were returned for a malformed instance id: %d", len(res.Tasks))
	}
}
