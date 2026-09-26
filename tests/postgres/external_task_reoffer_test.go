package postgres_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// The fetch offers an external task while it has retries left. A failure
// reported with none left raises an incident, and resolving the incident
// offers the task again. So a task at zero retries with no open incident at
// its step is off offer with nothing to say so. The release before this one
// left tasks that way, because its fetch offered them anyway, and a replica
// still running it during a rolling deploy leaves more. The sweep gives each
// the one try it had.
func TestAnExternalTaskAtZeroRetriesIsOfferedAgainUnlessAnIncidentHoldsIt(t *testing.T) {
	repo := repositories.NewRepository(testutils.StormConn(testutils.SetupTestDB(t)))
	ctx := entities.WithSystemContext(t.Context())

	park := func(retries int) uuid.UUID {
		t.Helper()
		task := &models.ExternalTaskModel{
			Base:                models.Base{ID: models.UUID(uuid.New())},
			ProjectID:           models.UUID(uuid.New()),
			ProcessInstanceID:   models.UUID(uuid.New()),
			ProcessDefinitionID: models.UUID(uuid.New()),
			NodeID:              "ship",
			Topic:               "shipping",
			Retries:             retries,
		}
		if err := repo.ExternalTask().Create(ctx, task); err != nil {
			t.Fatalf("park a task: %v", err)
		}
		return uuid.UUID(task.ID)
	}
	incidentAt := func(taskID uuid.UUID, status models.IncidentStatus) {
		t.Helper()
		task, err := repo.ExternalTask().Get(ctx, taskID)
		if err != nil {
			t.Fatalf("read the task: %v", err)
		}
		if _, err := repo.Incident().Create(ctx, models.IncidentModel{
			Base:       models.Base{ID: models.UUID(uuid.New())},
			InstanceID: task.ProcessInstanceID,
			NodeID:     task.NodeID,
			Status:     status,
			Error:      "the carrier refused the parcel",
		}); err != nil {
			t.Fatalf("raise an incident: %v", err)
		}
	}

	stranded := park(0)
	held := park(0)
	incidentAt(held, models.IncidentOpen)
	resolved := park(0)
	incidentAt(resolved, models.IncidentResolved)
	healthy := park(3)

	reoffered, err := repo.ExternalTask().ReofferStranded(ctx)
	if err != nil {
		t.Fatalf("re-offer: %v", err)
	}
	if reoffered != 2 {
		t.Errorf("re-offered %d tasks, want the stranded one and the one whose incident was resolved", reoffered)
	}

	offered, err := repo.ExternalTask().FetchAndLock(ctx, "shipping", "worker-1", 10, int64(time.Minute/time.Millisecond))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	var ids []uuid.UUID
	for _, task := range offered {
		ids = append(ids, uuid.UUID(task.ID))
		if uuid.UUID(task.ID) == stranded && task.Retries != 1 {
			t.Errorf("the stranded task came back with %d retries, want the one try it had", task.Retries)
		}
	}
	if slices.Contains(ids, held) {
		t.Error("a task an open incident holds was offered")
	}
	for name, id := range map[string]uuid.UUID{"stranded": stranded, "resolved": resolved, "healthy": healthy} {
		if !slices.Contains(ids, id) {
			t.Errorf("the %s task was not offered", name)
		}
	}
}

// It changes every tenant's tasks, so no tenant may start it.
func TestReofferingStrandedExternalTasksIsNotATenantsToDo(t *testing.T) {
	repo := repositories.NewRepository(testutils.StormConn(testutils.SetupTestDB(t)))
	ctx, _ := testutils.ScopedContext(t, repo)

	if _, err := repo.ExternalTask().ReofferStranded(ctx); !errors.Is(err, apierr.ErrForbidden) {
		t.Errorf("a tenant re-offered every tenant's external tasks: %v", err)
	}
}
