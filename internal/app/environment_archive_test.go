package app

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm/runtime/pgxdrv"
)

// Production run as an environment is archived as production run as an
// installation's own runtime is. A finished instance in an environment's
// database goes to that environment's archive, readable on its port, and
// nothing about it reaches the main database's.
func TestAnEnvironmentArchivesItsOwnFinishedInstances(t *testing.T) {
	conn := testutils.SetupTestDB(t)
	envDB := testutils.SetupTestDB(t)
	environmentID := uuid.New()
	conn.RegisterEnvironment(environmentID, envDB.Main())

	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	a := &App{storm: conn, repo: repo, sse: sse,
		svc: services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "env-archive-test", nil, nil, nil)}

	tenant, orgID, projectID := testutils.ScopedProject(t, repo)
	if err := SeedEnvironmentIdentity(t.Context(), envDB.Main(),
		models.ProjectModel{Base: models.Base{ID: models.FromUUID(projectID)}, OrganizationID: models.FromUUID(orgID), Name: "P"},
		models.OrganizationModel{Base: models.Base{ID: models.FromUUID(orgID)}, Name: "O"}); err != nil {
		t.Fatalf("seed the environment's identity rows: %v", err)
	}
	inEnv := db.Bind(tenant, environmentID)

	definitionID := uuid.Must(uuid.NewV7())
	if err := repo.Definition().Create(inEnv, models.ProcessDefinitionModel{
		Base: models.Base{ID: models.FromUUID(definitionID)}, ProjectID: models.FromUUID(projectID),
		Key: "claims", Name: "Claims", Version: 1,
	}); err != nil {
		t.Fatalf("deploy in the environment: %v", err)
	}
	instanceID := uuid.Must(uuid.NewV7())
	if _, err := repo.Process().Create(inEnv, models.ProcessInstanceModel{
		Base: models.Base{ID: models.FromUUID(instanceID)}, ProjectID: models.FromUUID(projectID),
		DefinitionID: models.FromUUID(definitionID), Status: models.ProcessCompleted,
	}); err != nil {
		t.Fatalf("start in the environment: %v", err)
	}
	old := processinstance.MutateKey(instanceID)
	old.SetUpdatedAt(time.Now().Add(-200 * 24 * time.Hour))
	if err := old.Update(inEnv, pgxdrv.Pool{P: envDB.Main()}); err != nil {
		t.Fatalf("date the instance: %v", err)
	}

	t.Setenv(envRetainFinishedInstancesDays, "")
	a.sweepRetention(entities.WithSystemContext(t.Context()), time.Now(), resolveOptInRetention())

	archived, err := repo.Archive().List(inEnv, projectID, contracts.ArchiveFilter{Limit: 10})
	if err != nil {
		t.Fatalf("read the environment's archive: %v", err)
	}
	if len(archived) != 1 || uuid.UUID(archived[0].ID) != instanceID {
		t.Fatalf("the environment's archive holds %v, want the finished instance", archived)
	}
	if _, _, err := repo.Archive().Get(inEnv, instanceID); err != nil {
		t.Fatalf("open the archived instance on the environment's port: %v", err)
	}
	live, err := repo.Process().Existing(entities.WithSystemContext(inEnv), []uuid.UUID{instanceID})
	if err != nil {
		t.Fatalf("read the environment's instances: %v", err)
	}
	if live[instanceID] {
		t.Fatal("the archived instance is still in the environment's live tables")
	}
	fromMain, err := repo.Archive().List(tenant, projectID, contracts.ArchiveFilter{Limit: 10})
	if err != nil {
		t.Fatalf("read the main archive: %v", err)
	}
	if len(fromMain) != 0 {
		t.Fatalf("the main database's archive holds %d instances from the environment", len(fromMain))
	}
}
