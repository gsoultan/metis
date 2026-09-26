package environment_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/app"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/gorms"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// TestAProcessRunsInAnEnvironment is the whole feature, end to end: deploy a
// model on an environment's port, start it, and find nothing in the main
// database.
//
// It failed twice before it passed, both times for reasons no unit test would
// have shown. The unit of work opened its transaction on the main connection,
// so every read went to the environment and every write to main — a model
// deployed on staging landed in production's tables and was invisible to
// staging. And tenant scoping is a join against `projects`, which is empty in a
// fresh environment database, so every scoped read matched nothing and every
// create was refused with "record not found".
func TestAProcessRunsInAnEnvironment(t *testing.T) {
	mainDB, conn := testutils.SetupTestStore(t)
	stagingDB := testutils.SetupTestDB(t)
	t.Cleanup(gorms.ResetEnvironmentDBs)

	environmentID := uuid.New()
	// Both layers, the way the boot sequence registers them: a half-open
	// environment reaches one and refuses on the other.
	gorms.RegisterEnvironmentDB(environmentID, stagingDB)
	if pool := testutils.StormConn(stagingDB); pool != nil {
		conn.RegisterEnvironment(environmentID, pool.Main())
	}

	repo := repositories.NewRepository(testutils.StormConn(mainDB))
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "env-e2e", nil, nil, nil)

	ctx := context.Background()
	org, err := svc.CreateOrganization(ctx, "Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	tenantCtx := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})
	project, err := svc.CreateProject(tenantCtx, org.ID, "P", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// Exactly what opening an environment does at boot. Called rather than
	// reproduced, so a change to the real seeding is covered by this test.
	if err := app.SeedEnvironmentIdentity(ctx, stagingDB,
		models.ProjectModel{
			Base:           models.Base{ID: models.FromUUID(project.ID)},
			OrganizationID: models.FromUUID(org.ID),
			Name:           project.Name,
		},
		models.OrganizationModel{
			Base: models.Base{ID: models.FromUUID(org.ID)},
			Name: org.Name,
		}); err != nil {
		t.Fatalf("seed the environment's identity rows: %v", err)
	}

	// Everything from here happens on the staging port.
	stagingCtx := db.Bind(tenantCtx, environmentID)

	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: project.ID},
		Key:     "in-staging",
		Name:    "In staging",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: "approve", Name: "Approve", Type: entities.UserTask, Assignee: "ada", Incoming: []string{"f1"}, Outgoing: []string{"f2"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "end"},
		},
	}
	created, err := svc.CreateDefinition(stagingCtx, def)
	if err != nil {
		t.Fatalf("deploy a model on the staging port: %T %+v", err, err)
	}
	_ = created

	instance, err := svc.StartProcess(stagingCtx, project.ID, "in-staging", map[string]any{"amount": 10})
	if err != nil {
		t.Fatalf("start a process on the staging port: %v", err)
	}
	if instance == uuid.Nil {
		t.Fatal("starting a process on the staging port returned no instance")
	}

	// And it must be absent from the main database.
	fromMain, err := repo.Definition().List(entities.WithSystemContext(ctx))
	if err != nil {
		t.Fatalf("list definitions on main: %v", err)
	}
	if len(fromMain) != 0 {
		t.Fatalf("the main database has %d definitions; the model was deployed to staging", len(fromMain))
	}
}

// TestSeedingIsIdempotent covers the boot path running twice, which is what
// every restart is.
func TestSeedingIsIdempotent(t *testing.T) {
	stagingDB := testutils.SetupTestDB(t)
	ctx := context.Background()

	orgID := uuid.New()
	projectID := uuid.New()
	project := models.ProjectModel{
		Base:           models.Base{ID: models.FromUUID(projectID)},
		OrganizationID: models.FromUUID(orgID),
		Name:           "Purchase Approval",
	}
	organization := models.OrganizationModel{
		Base: models.Base{ID: models.FromUUID(orgID)},
		Name: "Acme",
	}

	for range 2 {
		if err := app.SeedEnvironmentIdentity(ctx, stagingDB, project, organization); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	var projects int64
	if err := stagingDB.Model(&models.ProjectModel{}).Count(&projects).Error; err != nil {
		t.Fatalf("count projects: %v", err)
	}
	if projects != 1 {
		t.Fatalf("seeding twice left %d project rows; a restart must not duplicate them", projects)
	}

	// A rename in the main database reaches the copy on the next boot.
	project.Name = "Purchase Approval (EU)"
	if err := app.SeedEnvironmentIdentity(ctx, stagingDB, project, organization); err != nil {
		t.Fatalf("reseed after a rename: %v", err)
	}
	var stored models.ProjectModel
	if err := stagingDB.First(&stored, "id = ?", models.FromUUID(projectID)).Error; err != nil {
		t.Fatalf("read back the project: %v", err)
	}
	if stored.Name != "Purchase Approval (EU)" {
		t.Fatalf("the copied project is still called %q after a rename", stored.Name)
	}
}
