package project_test

import (
	"testing"

	"github.com/gsoultan/gobpm/server/domains/entities"
	handlersimpl "github.com/gsoultan/gobpm/server/domains/handlers/impl"
	"github.com/gsoultan/gobpm/server/domains/observers/impl"
	"github.com/gsoultan/gobpm/server/domains/services"
	service_impl2 "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
)

func TestProjectAssociation(t *testing.T) {
	ctx := t.Context()
	db := testutils.SetupTestDB(t)

	repo := repositories.NewRepository(db)
	dispatcher := impl.NewEventDispatcher()

	orgSvc := service_impl2.NewOrganizationService(repo)
	projectSvc := service_impl2.NewProjectService(repo)
	defSvc := service_impl2.NewDefinitionService(repo)
	connectorSvc := service_impl2.NewConnectorService(repo)
	engine := service_impl2.NewExecutionEngine(repo, dispatcher)
	taskSvc := service_impl2.NewTaskService(repo, engine, service_impl2.NewAuditWriter(repo.Audit()))
	jobSvc := service_impl2.NewJobService(repo, engine, connectorSvc, service_impl2.NewNoOpLocker(), handlersimpl.NewErrorBoundaryMatcher())
	externalTaskSvc := service_impl2.NewExternalTaskService(repo, engine)
	decisionSvc := service_impl2.NewDecisionService(repo, service_impl2.NewDecisionTableEvaluator(service_impl2.NewFEELEvaluator()))
	migrationSvc := service_impl2.NewMigrationService(repo)
	sse := impl.NewSSEObserver()
	collaborationSvc := service_impl2.NewCollaborationService(sse)

	handlerFactory := handlersimpl.NewNodeHandlerFactory(engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc, service_impl2.NewFEELEvaluator(), repo.Subscription())
	engine.Apply(
		service_impl2.WithHandlerFactory(handlerFactory),
		service_impl2.WithJobService(jobSvc),
	)

	messagingSvc := service_impl2.NewMessagingService(engine, externalTaskSvc)
	userSvc := service_impl2.NewUserService(repo, "test-jwt-secret")
	setupSvc := service_impl2.NewSetupService(nil)
	svc := services.NewService(services.ServiceParams{
		OrganizationService:  orgSvc,
		ProjectService:       projectSvc,
		DefinitionService:    defSvc,
		TaskService:          taskSvc,
		ExecutionEngine:      engine,
		JobService:           jobSvc,
		ExternalTaskService:  externalTaskSvc,
		DecisionService:      decisionSvc,
		MigrationService:     migrationSvc,
		ConnectorService:     connectorSvc,
		CollaborationService: collaborationSvc,
		MessagingService:     messagingSvc,
		UserService:          userSvc,
		SetupService:         setupSvc,
	})

	// CreateAuditEntry an organization
	org, _ := svc.CreateOrganization(ctx, "Test Org", "")

	// CreateAuditEntry two projects
	proj1, _ := svc.CreateProject(ctx, org.ID, "Project 1", "")
	proj2, _ := svc.CreateProject(ctx, org.ID, "Project 2", "")

	// CreateAuditEntry a definition in each project
	def1 := entities.ProcessDefinition{
		Project: &entities.Project{ID: proj1.ID},
		Key:     "proc1",
		Name:    "Process 1",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "f1", SourceRef: "start", TargetRef: "end"}},
	}
	_, _ = svc.CreateDefinition(ctx, &def1)

	def2 := entities.ProcessDefinition{
		Project: &entities.Project{ID: proj2.ID},
		Key:     "proc2",
		Name:    "Process 2",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{{ID: "f1", SourceRef: "start", TargetRef: "end"}},
	}
	_, _ = svc.CreateDefinition(ctx, &def2)

	// ListConnectors definitions by project
	defs1, _ := svc.ListDefinitions(ctx, proj1.ID)
	if len(defs1) != 1 || defs1[0].Key != "proc1" {
		t.Errorf("expected 1 definition for proj1 (proc1), got %v", defs1)
	}

	defs2, _ := svc.ListDefinitions(ctx, proj2.ID)
	if len(defs2) != 1 || defs2[0].Key != "proc2" {
		t.Errorf("expected 1 definition for proj2 (proc2), got %v", defs2)
	}

	// Start instances
	_, _ = svc.StartProcess(ctx, proj1.ID, "proc1", nil)
	_, _ = svc.StartProcess(ctx, proj2.ID, "proc2", nil)

	// ListConnectors instances by project
	insts1, _ := svc.ListInstances(ctx, proj1.ID)
	if len(insts1) != 1 || insts1[0].Project.ID != proj1.ID {
		t.Errorf("expected 1 instance for proj1, got %v", insts1)
	}

	insts2, _ := svc.ListInstances(ctx, proj2.ID)
	if len(insts2) != 1 || insts2[0].Project.ID != proj2.ID {
		t.Errorf("expected 1 instance for proj2, got %v", insts2)
	}
}
