package bpmn_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	handlersimpl "github.com/gsoultan/metis/server/domains/handlers/impl"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	service_impl2 "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

func TestBPMNEvents(t *testing.T) {
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

	handlerFactory := handlersimpl.NewNodeHandlerFactory(engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc, repo.Subscription(), service_impl2.NewAuditWriter(repo.Audit()))
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

	org, _ := svc.CreateOrganization(ctx, "Test Org", "")
	proj, _ := svc.CreateProject(ctx, org.ID, "Event Project", "")

	t.Run("ErrorEndEvent", func(t *testing.T) {
		def := entities.ProcessDefinition{
			Project: &entities.Project{ID: proj.ID},
			Key:     "error-process",
			Nodes: []*entities.Node{
				{ID: "start", Type: entities.StartEvent},
				{ID: "sub", Type: entities.SubProcess, Nodes: []*entities.Node{
					{ID: "sub-start", Type: entities.StartEvent, ParentID: "sub"},
					{ID: "error-end", Type: entities.ErrorEndEvent, ParentID: "sub", Properties: map[string]any{"error_code": "ERROR_1"}},
				}, Flows: []*entities.SequenceFlow{
					{ID: "sf1", SourceRef: "sub-start", TargetRef: "error-end"},
				}},
				{ID: "error-catch", Type: entities.BoundaryEvent, AttachedToRef: "sub", Properties: map[string]any{"error_code": "ERROR_1"}},
				{ID: "task-after-error", Type: entities.UserTask, Name: "Task After Error"},
				{ID: "end", Type: entities.EndEvent},
			},
			Flows: []*entities.SequenceFlow{
				{ID: "f1", SourceRef: "start", TargetRef: "sub"},
				{ID: "f2", SourceRef: "sub", TargetRef: "end"},
				{ID: "f3", SourceRef: "error-catch", TargetRef: "task-after-error"},
				{ID: "f4", SourceRef: "task-after-error", TargetRef: "end"},
			},
		}
		defRes, err := svc.CreateDefinition(ctx, &def)
		if err != nil {
			t.Fatalf("failed to create definition: %v", err)
		}
		instanceID, err := svc.StartProcess(ctx, proj.ID, "error-process", nil)
		if err != nil {
			t.Fatalf("failed to start process: %v", err)
		}
		_ = defRes

		tasks, _ := svc.ListTasks(ctx, proj.ID)
		found := false
		for _, task := range tasks {
			if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == "task-after-error" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected task-after-error to be active")
		}
	})

	t.Run("EscalationEvent", func(t *testing.T) {
		def := entities.ProcessDefinition{
			Project: &entities.Project{ID: proj.ID},
			Key:     "escalation-process",
			Nodes: []*entities.Node{
				{ID: "start", Type: entities.StartEvent},
				{ID: "sub", Type: entities.SubProcess, Nodes: []*entities.Node{
					{ID: "sub-start", Type: entities.StartEvent, ParentID: "sub"},
					{ID: "esc-throw", Type: entities.EscalationThrowEvent, ParentID: "sub", Properties: map[string]any{"escalation_code": "ESC_1"}},
					{ID: "sub-end", Type: entities.EndEvent, ParentID: "sub"},
				}, Flows: []*entities.SequenceFlow{
					{ID: "sf1", SourceRef: "sub-start", TargetRef: "esc-throw"},
					{ID: "sf2", SourceRef: "esc-throw", TargetRef: "sub-end"},
				}},
				{ID: "esc-catch", Type: entities.BoundaryEvent, AttachedToRef: "sub", Properties: map[string]any{"escalation_code": "ESC_1"}},
				{ID: "task-after-esc", Type: entities.UserTask, Name: "Task After Escalation"},
				{ID: "end", Type: entities.EndEvent},
			},
			Flows: []*entities.SequenceFlow{
				{ID: "f1", SourceRef: "start", TargetRef: "sub"},
				{ID: "f2", SourceRef: "sub", TargetRef: "end"},
				{ID: "f3", SourceRef: "esc-catch", TargetRef: "task-after-esc"},
				{ID: "f4", SourceRef: "task-after-esc", TargetRef: "end"},
			},
		}
		if _, err := svc.CreateDefinition(ctx, &def); err != nil {
			t.Fatalf("failed to create definition: %v", err)
		}
		instanceID, err := svc.StartProcess(ctx, proj.ID, "escalation-process", nil)
		if err != nil {
			t.Fatalf("failed to start process: %v", err)
		}

		tasks, _ := svc.ListTasks(ctx, proj.ID)
		found := false
		for _, task := range tasks {
			if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == "task-after-esc" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected task-after-esc to be active")
		}
	})

	t.Run("CompensationEvent", func(t *testing.T) {
		def := entities.ProcessDefinition{
			Project: &entities.Project{ID: proj.ID},
			Key:     "comp-process",
			Nodes: []*entities.Node{
				{ID: "start", Type: entities.StartEvent},
				{ID: "task1", Type: entities.UserTask, Name: "Task 1"},
				{ID: "comp-boundary", Type: entities.BoundaryEvent, AttachedToRef: "task1", Properties: map[string]any{"compensation": "true"}},
				{ID: "comp-handler", Type: entities.UserTask, Name: "Compensation Handler"},
				{ID: "comp-throw", Type: entities.CompensationThrowEvent},
				{ID: "end", Type: entities.EndEvent},
			},
			Flows: []*entities.SequenceFlow{
				{ID: "f1", SourceRef: "start", TargetRef: "task1"},
				{ID: "f2", SourceRef: "task1", TargetRef: "comp-throw"},
				{ID: "f3", SourceRef: "comp-throw", TargetRef: "end"},
				{ID: "f4", SourceRef: "comp-boundary", TargetRef: "comp-handler"},
			},
		}
		if _, err := svc.CreateDefinition(ctx, &def); err != nil {
			t.Fatalf("failed to create definition: %v", err)
		}
		instanceID, err := svc.StartProcess(ctx, proj.ID, "comp-process", nil)
		if err != nil {
			t.Fatalf("failed to start process: %v", err)
		}

		// Complete Task 1
		tasks, _ := svc.ListTasks(ctx, proj.ID)
		for _, task := range tasks {
			if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == "task1" {
				_ = svc.CompleteTask(ctx, task.ID, "test-user", nil)
			}
		}

		// Now comp-throw should have executed, triggering comp-handler
		tasks, _ = svc.ListTasks(ctx, proj.ID)
		found := false
		for _, task := range tasks {
			if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == "comp-handler" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected compensation-handler to be active")
		}
	})
}
