package handlers_test

import (
	"testing"
	"time"

	"github.com/gsoultan/gobpm/server/domains/entities"
	handlersimpl "github.com/gsoultan/gobpm/server/domains/handlers/impl"
	"github.com/gsoultan/gobpm/server/domains/observers/impl"
	"github.com/gsoultan/gobpm/server/domains/services"
	service_impl2 "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
)

func TestInclusiveGateway(t *testing.T) {
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
	jobSvc := testutils.NewSynchronousJobService(engine, repo)
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

	org, _ := svc.CreateOrganization(ctx, "Test Org", "")
	proj, _ := svc.CreateProject(ctx, org.ID, "Inclusive Project", "")

	// Start -> InclusiveGateway -> (TaskA if condA, TaskB if condB) -> End
	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: proj.ID},
		Key:     "inclusive-process",
		Name:    "Inclusive Gateway Process",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "inclusive", Type: entities.InclusiveGateway},
			{ID: "taskA", Type: entities.UserTask, Name: "Task A"},
			{ID: "taskB", Type: entities.UserTask, Name: "Task B"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "inclusive"},
			{ID: "fA", SourceRef: "inclusive", TargetRef: "taskA", Condition: "condA"},
			{ID: "fB", SourceRef: "inclusive", TargetRef: "taskB", Condition: "condB"},
			{ID: "fEndA", SourceRef: "taskA", TargetRef: "end"},
			{ID: "fEndB", SourceRef: "taskB", TargetRef: "end"},
		},
	}

	_, _ = svc.CreateDefinition(ctx, &def)

	// Case 1: Both true
	instanceID, _ := svc.StartProcess(ctx, proj.ID, "inclusive-process", map[string]any{"condA": true, "condB": true})
	tasks, _ := svc.ListTasks(ctx, proj.ID)

	// Should have 2 tasks
	count := 0
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && (task.Status == entities.TaskUnclaimed || task.Status == entities.TaskClaimed) {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 tasks for both true, got %d", count)
	}

	// Case 2: Only A true
	proj2, _ := svc.CreateProject(ctx, org.ID, "Inclusive Project 2", "")
	def.Project = &entities.Project{ID: proj2.ID}
	_, _ = svc.CreateDefinition(ctx, &def)
	instanceID2, _ := svc.StartProcess(ctx, proj2.ID, "inclusive-process", map[string]any{"condA": true, "condB": false})
	tasks2, _ := svc.ListTasks(ctx, proj2.ID)

	count = 0
	for _, task := range tasks2 {
		if task.Instance != nil && task.Instance.ID == instanceID2 && (task.Status == entities.TaskUnclaimed || task.Status == entities.TaskClaimed) {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 task for only A true, got %d", count)
	}
}

func TestTimerEvent(t *testing.T) {
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
	jobSvc := testutils.NewSynchronousJobService(engine, repo)
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

	org, _ := svc.CreateOrganization(ctx, "Test Org", "")
	proj, _ := svc.CreateProject(ctx, org.ID, "Timer Project", "")

	// Start -> Timer (100ms) -> End
	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: proj.ID},
		Key:     "timer-process",
		Name:    "Timer Process",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "timer", Type: entities.IntermediateCatchEvent, Condition: "100ms"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "timer"},
			{ID: "f2", SourceRef: "timer", TargetRef: "end"},
		},
	}

	_, _ = svc.CreateDefinition(ctx, &def)

	start := time.Now()
	instanceID, _ := svc.StartProcess(ctx, proj.ID, "timer-process", nil)
	elapsed := time.Since(start)

	if elapsed < 100*time.Millisecond {
		t.Errorf("expected timer to wait at least 100ms, took %v", elapsed)
	}

	instance, _ := svc.GetInstance(ctx, instanceID)
	if instance.Status != "completed" {
		t.Errorf("expected instance to be completed, got %s", instance.Status)
	}
}

func TestServiceTask(t *testing.T) {
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
	jobSvc := testutils.NewSynchronousJobService(engine, repo)
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

	org, _ := svc.CreateOrganization(ctx, "Test Org", "")
	proj, _ := svc.CreateProject(ctx, org.ID, "Service Project", "")

	// Start -> ServiceTask -> End
	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: proj.ID},
		Key:     "service-process",
		Name:    "Service Process",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "service", Type: entities.ServiceTask, Name: "My Service"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "service"},
			{ID: "f2", SourceRef: "service", TargetRef: "end"},
		},
	}

	_, _ = svc.CreateDefinition(ctx, &def)

	instanceID, _ := svc.StartProcess(ctx, proj.ID, "service-process", nil)

	instance, _ := svc.GetInstance(ctx, instanceID)
	if instance.Variables["service_completed"] != true {
		t.Errorf("expected service_completed variable to be true, got %v", instance.Variables["service_completed"])
	}
}

func TestAdvancedTasks(t *testing.T) {
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
	jobSvc := testutils.NewSynchronousJobService(engine, repo)
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

	org, _ := svc.CreateOrganization(ctx, "Test Org", "")
	proj, _ := svc.CreateProject(ctx, org.ID, "Advanced Project", "")

	t.Run("ManualTask Creates Task Entry", func(t *testing.T) {
		def := entities.ProcessDefinition{
			Project: &entities.Project{ID: proj.ID},
			Key:     "manual-process",
			Name:    "Manual Process",
			Nodes: []*entities.Node{
				{ID: "start", Type: entities.StartEvent},
				{ID: "manual", Type: entities.ManualTask, Properties: map[string]any{"name": "My Manual Action"}},
				{ID: "end", Type: entities.EndEvent},
			},
			Flows: []*entities.SequenceFlow{
				{ID: "f1", SourceRef: "start", TargetRef: "manual"},
				{ID: "f2", SourceRef: "manual", TargetRef: "end"},
			},
		}
		_, _ = svc.CreateDefinition(ctx, &def)

		instanceID, _ := svc.StartProcess(ctx, proj.ID, "manual-process", nil)

		tasks, _ := svc.ListTasks(ctx, proj.ID)
		var manualTask *entities.Task
		for _, tk := range tasks {
			if tk.Instance != nil && tk.Instance.ID == instanceID && tk.NodeID() == "manual" {
				manualTask = &tk
				break
			}
		}

		if manualTask == nil {
			t.Fatal("Expected manual task to be created")
		}
		if manualTask.Type != entities.ManualTask {
			t.Errorf("Expected task type %s, got %s", entities.ManualTask, manualTask.Type)
		}

		// Complete it
		_ = svc.CompleteTask(ctx, manualTask.ID, "manager", nil)
		instance, _ := svc.GetInstance(ctx, instanceID)
		if instance.Status != "completed" {
			t.Errorf("Expected instance to be completed after manual task, got %s", instance.Status)
		}
	})

	t.Run("BusinessRuleTask Evaluates DMN", func(t *testing.T) {
		decision := entities.DecisionDefinition{
			Project: &entities.Project{ID: proj.ID},
			Key:     "calc-discount",
			Inputs:  []entities.DecisionInput{{ID: "in1", Label: "amount", Expression: "amount", Type: "number"}},
			Outputs: []entities.DecisionOutput{{ID: "out1", Name: "discount", Type: "number"}},
			Rules: []entities.DecisionRule{
				{Inputs: []string{"> 100"}, Outputs: []any{0.1}},
				{Inputs: []string{"<= 100"}, Outputs: []any{0.05}},
			},
		}
		_, _ = svc.CreateDecision(ctx, decision)

		def := entities.ProcessDefinition{
			Project: &entities.Project{ID: proj.ID},
			Key:     "rule-process",
			Name:    "Rule Process",
			Nodes: []*entities.Node{
				{ID: "start", Type: entities.StartEvent},
				{
					ID:   "rule",
					Type: entities.BusinessRuleTask,
					Properties: map[string]any{
						"decision_key":   "calc-discount",
						"input_mapping":  map[string]any{"amount": "total"},
						"output_mapping": map[string]any{"final_discount": "discount"},
					},
				},
				{ID: "end", Type: entities.EndEvent},
			},
			Flows: []*entities.SequenceFlow{
				{ID: "f1", SourceRef: "start", TargetRef: "rule"},
				{ID: "f2", SourceRef: "rule", TargetRef: "end"},
			},
		}
		_, _ = svc.CreateDefinition(ctx, &def)

		instanceID, _ := svc.StartProcess(ctx, proj.ID, "rule-process", map[string]any{"total": 150})
		instance, _ := svc.GetInstance(ctx, instanceID)

		if instance.Variables["final_discount"] != 0.1 {
			t.Errorf("Expected final_discount 0.1, got %v", instance.Variables["final_discount"])
		}
	})
}
