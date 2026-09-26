package postgres_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	handlersimpl "github.com/gsoultan/metis/server/domains/handlers/impl"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// tasksAhead is as many external tasks as one read of the generated store
// returns: storm starts every query with a limit of 1000.
const tasksAhead = 1000

// Resolving an incident on an external task offers that task again, however
// many external tasks its instance has had.
//
// The retry found the task by reading the instance's external tasks, oldest
// first, through a query the store caps at a thousand rows. An instance that
// has sent a thousand items to workers — one external task per line of a large
// order — had its newest task outside the window: the incident was marked
// resolved and the task stayed off offer until a sweep came round to it.
func TestResolvingAnIncidentOffersItsExternalTaskAgainPastAThousand(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	ctx := entities.WithSystemContext(t.Context())
	engine := serviceimpl.NewExecutionEngine(repo, observersimpl.NewEventDispatcher())
	jobs := serviceimpl.NewJobService(repo, engine, serviceimpl.NewConnectorService(repo),
		serviceimpl.NewNoOpLocker(), handlersimpl.NewErrorBoundaryMatcher())

	instanceID, projectID, definitionID := uuid.New(), uuid.New(), uuid.New()
	if err := db.WithContext(ctx).Exec(`
		INSERT INTO external_tasks (id, created_at, updated_at, project_id, instance_id, definition_id,
		                            node_id, topic, retries, retry_timeout, variables)
		SELECT gen_random_uuid(), now() - interval '1 hour', now() - interval '1 hour', ?, ?, ?,
		       'pick', 'warehouse', 3, 0, '{}'
		  FROM generate_series(1, ?) AS n`, projectID, instanceID, definitionID, tasksAhead).Error; err != nil {
		t.Fatalf("seed %d earlier tasks: %v", tasksAhead, err)
	}
	stranded := &models.ExternalTaskModel{
		Base:                models.Base{ID: models.UUID(uuid.New())},
		ProjectID:           models.UUID(projectID),
		ProcessInstanceID:   models.UUID(instanceID),
		ProcessDefinitionID: models.UUID(definitionID),
		NodeID:              "ship",
		Topic:               "shipping",
		Retries:             0,
	}
	if err := repo.ExternalTask().Create(ctx, stranded); err != nil {
		t.Fatalf("park the failed task: %v", err)
	}
	incident, err := repo.Incident().Create(ctx, models.IncidentModel{
		Base:       models.Base{ID: models.UUID(uuid.New())},
		InstanceID: models.UUID(instanceID),
		NodeID:     "ship",
		Status:     models.IncidentOpen,
		Error:      "the carrier refused the parcel",
	})
	if err != nil {
		t.Fatalf("raise the incident: %v", err)
	}

	if err := jobs.ResolveIncident(ctx, uuid.UUID(incident.ID)); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	after, err := repo.ExternalTask().Get(ctx, uuid.UUID(stranded.ID))
	if err != nil {
		t.Fatalf("read the task back: %v", err)
	}
	if after.Retries != 1 {
		t.Fatalf("the incident was resolved and its task behind %d others has %d retries; it should be offered again",
			tasksAhead, after.Retries)
	}
}
