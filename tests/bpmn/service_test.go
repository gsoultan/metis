package bpmn_test

import (
	"fmt"
	"testing"

	"github.com/gsoultan/gobpm/server/domains/entities"
	handlersimpl "github.com/gsoultan/gobpm/server/domains/handlers/impl"
	"github.com/gsoultan/gobpm/server/domains/observers/impl"
	"github.com/gsoultan/gobpm/server/domains/services"
	service_impl2 "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
)

func TestBPMNFlow(t *testing.T) {
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

	org, _ := svc.CreateOrganization(ctx, "Test Org", "")
	proj, _ := svc.CreateProject(ctx, org.ID, "Test Project", "")

	// CreateAuditEntry a BPMN definition: Start -> Task1 -> End
	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: proj.ID},
		Key:     "test-process",
		Name:    "Test Process",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "task1", Type: entities.UserTask, Name: "User Task", Assignee: "user1"},
			{ID: "end", Type: entities.EndEvent, Name: "End"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "task1"},
			{ID: "f2", SourceRef: "task1", TargetRef: "end"},
		},
	}

	_, err := svc.CreateDefinition(ctx, &def)
	if err != nil {
		t.Fatalf("failed to create definition: %v", err)
	}

	// Start process
	instanceID, err := svc.StartProcess(ctx, proj.ID, "test-process", nil)
	if err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	// Check instance state
	instance, err := svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("failed to get instance: %v", err)
	}

	if instance.Status != "active" {
		t.Errorf("expected status active, got %s", instance.Status)
	}

	// Should have task1 active
	tasks, err := svc.ListTasks(ctx, proj.ID)
	if err != nil {
		t.Fatalf("failed to list tasks: %v", err)
	}

	if len(tasks) != 1 {
		t.Errorf("expected 1 task, got %d", len(tasks))
	} else if tasks[0].NodeID() != "task1" {
		t.Errorf("expected task for node task1, got %s", tasks[0].NodeID())
	}

	// Complete task1
	err = svc.CompleteTask(ctx, tasks[0].ID, "user1", nil)
	if err != nil {
		t.Fatalf("failed to complete task: %v", err)
	}

	// Instance should be completed now (EndEvent reached)
	instance, err = svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("failed to get instance: %v", err)
	}

	if instance.Status != "completed" {
		t.Errorf("expected status completed, got %s", instance.Status)
	}
}

func TestExclusiveGatewayFlow(t *testing.T) {
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

	org, _ := svc.CreateOrganization(ctx, "Test Org", "")
	proj, _ := svc.CreateProject(ctx, org.ID, "Exclusive Project", "")

	// Start -> Gateway -> TaskA (if approved) OR TaskB (if rejected) -> End
	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: proj.ID},
		Key:     "exclusive-process",
		Name:    "Exclusive Gateway Process",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "gateway", Type: entities.ExclusiveGateway, Name: "Approval Gateway"},
			{ID: "taskA", Type: entities.UserTask, Name: "Task Approved"},
			{ID: "taskB", Type: entities.UserTask, Name: "Task Rejected"},
			{ID: "end", Type: entities.EndEvent, Name: "End"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "gateway"},
			{ID: "fA", SourceRef: "gateway", TargetRef: "taskA", Condition: "approved"},
			{ID: "fB", SourceRef: "gateway", TargetRef: "taskB", Condition: "rejected"},
			{ID: "fEndA", SourceRef: "taskA", TargetRef: "end"},
			{ID: "fEndB", SourceRef: "taskB", TargetRef: "end"},
		},
	}

	_, err := svc.CreateDefinition(ctx, &def)
	if err != nil {
		t.Fatalf("failed to create definition: %v", err)
	}

	// Case 1: Approved
	_, err = svc.StartProcess(ctx, proj.ID, "exclusive-process", map[string]any{"approved": true})
	if err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	tasks, _ := svc.ListTasks(ctx, proj.ID)
	if len(tasks) != 1 || tasks[0].NodeID() != "taskA" {
		t.Errorf("expected taskA to be created, got %v", tasks)
	}

	// Case 2: Rejected (I'll use another project to isolate)
	proj2, _ := svc.CreateProject(ctx, org.ID, "Exclusive Project 2", "")
	def.Project = &entities.Project{ID: proj2.ID}
	_, _ = svc.CreateDefinition(ctx, &def)

	_, err = svc.StartProcess(ctx, proj2.ID, "exclusive-process", map[string]any{"rejected": true})
	if err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	tasks, _ = svc.ListTasks(ctx, proj2.ID)
	if len(tasks) != 1 || tasks[0].NodeID() != "taskB" {
		t.Errorf("expected taskB to be created, got %v", tasks)
	}
}

func TestParallelGatewayJoin(t *testing.T) {
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

	org, _ := svc.CreateOrganization(ctx, "Test Org", "")
	proj, _ := svc.CreateProject(ctx, org.ID, "Parallel Project", "")

	// Start -> Fork -> (TaskA, TaskB) -> Join -> End
	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: proj.ID},
		Key:     "parallel-join",
		Name:    "Parallel Join Process",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "fork", Type: entities.ParallelGateway},
			{ID: "taskA", Type: entities.UserTask, Name: "Task A"},
			{ID: "taskB", Type: entities.UserTask, Name: "Task B"},
			{ID: "join", Type: entities.ParallelGateway},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "fork"},
			{ID: "fA", SourceRef: "fork", TargetRef: "taskA"},
			{ID: "fB", SourceRef: "fork", TargetRef: "taskB"},
			{ID: "fJ1", SourceRef: "taskA", TargetRef: "join"},
			{ID: "fJ2", SourceRef: "taskB", TargetRef: "join"},
			{ID: "fEnd", SourceRef: "join", TargetRef: "end"},
		},
	}

	_, _ = svc.CreateDefinition(ctx, &def)
	instanceID, _ := svc.StartProcess(ctx, proj.ID, "parallel-join", nil)

	tasks, _ := svc.ListTasks(ctx, proj.ID)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	// Complete Task A
	var taskA entities.Task
	for _, t := range tasks {
		if t.NodeID() == "taskA" {
			taskA = t
		}
	}
	_ = svc.CompleteTask(ctx, taskA.ID, "", nil)

	// Instance should still be active, waiting at Join
	instance, _ := svc.GetInstance(ctx, instanceID)
	if instance.Status != "active" {
		t.Errorf("expected instance to be active, got %s", instance.Status)
	}

	// Complete Task B
	var taskB entities.Task
	tasks, _ = svc.ListTasks(ctx, proj.ID)
	for _, t := range tasks {
		if t.NodeID() == "taskB" && (t.Status == entities.TaskUnclaimed || t.Status == entities.TaskClaimed) {
			taskB = t
		}
	}
	_ = svc.CompleteTask(ctx, taskB.ID, "", nil)

	// Now instance should be completed
	instance, _ = svc.GetInstance(ctx, instanceID)
	if instance.Status != "completed" {
		t.Errorf("expected instance to be completed after both tasks, got %s", instance.Status)
	}
}

func TestParallelGatewayFlow(t *testing.T) {
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

	org, _ := svc.CreateOrganization(ctx, "Test Org", "")
	proj, _ := svc.CreateProject(ctx, org.ID, "Parallel Flow Project", "")

	// Start -> ParallelGateway -> (TaskA AND TaskB) -> End
	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: proj.ID},
		Key:     "parallel-process",
		Name:    "Parallel Gateway Process",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "parallel", Type: entities.ParallelGateway, Name: "Fork"},
			{ID: "taskA", Type: entities.UserTask, Name: "Task A"},
			{ID: "taskB", Type: entities.UserTask, Name: "Task B"},
			{ID: "end", Type: entities.EndEvent, Name: "End"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "parallel"},
			{ID: "fA", SourceRef: "parallel", TargetRef: "taskA"},
			{ID: "fB", SourceRef: "parallel", TargetRef: "taskB"},
			{ID: "fEndA", SourceRef: "taskA", TargetRef: "end"},
			{ID: "fEndB", SourceRef: "taskB", TargetRef: "end"},
		},
	}

	_, _ = svc.CreateDefinition(ctx, &def)
	_, _ = svc.StartProcess(ctx, proj.ID, "parallel-process", nil)

	tasks, _ := svc.ListTasks(ctx, proj.ID)
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestTaskServiceEnhancements(t *testing.T) {
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

	org, _ := svc.CreateOrganization(ctx, "Test Org", "")
	proj, _ := svc.CreateProject(ctx, org.ID, "Test Project", "")

	// CreateAuditEntry a BPMN definition: Start -> Task1 -> End
	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: proj.ID},
		Key:     "test-process",
		Name:    "Test Process",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "task1", Type: entities.UserTask, Name: "User Task", Documentation: "Complete this task carefully."},
			{ID: "end", Type: entities.EndEvent, Name: "End"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "task1"},
			{ID: "f2", SourceRef: "task1", TargetRef: "end"},
		},
	}

	_, _ = svc.CreateDefinition(ctx, &def)
	_, _ = svc.StartProcess(ctx, proj.ID, "test-process", nil)

	tasks, _ := svc.ListTasks(ctx, proj.ID)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.Description != "Complete this task carefully." {
		t.Errorf("expected description 'Complete this task carefully.', got '%s'", task.Description)
	}

	// Test UpdateTask
	task.Name = "Updated Task Name"
	task.Priority = 80
	err := svc.UpdateTask(ctx, task)
	if err != nil {
		t.Fatalf("failed to update task: %v", err)
	}

	updated, _ := svc.GetTask(ctx, task.ID)
	if updated.Name != "Updated Task Name" {
		t.Errorf("expected name 'Updated Task Name', got '%s'", updated.Name)
	}
	if updated.Priority != 80 {
		t.Errorf("expected priority 80, got %d", updated.Priority)
	}

	// Test AssignTask
	err = svc.AssignTask(ctx, task.ID, "new-user")
	if err != nil {
		t.Fatalf("failed to assign task: %v", err)
	}

	assigned, _ := svc.GetTask(ctx, task.ID)
	if assigned.Assignee == nil || assigned.Assignee.Username != "new-user" {
		t.Errorf("Expected assignee 'new-user', got '%v'", assigned.Assignee)
	}
	if assigned.Status != entities.TaskClaimed {
		t.Errorf("expected status 'claimed', got '%s'", assigned.Status)
	}
}

func TestExecutionEnhancements(t *testing.T) {
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

	// Test ExecuteScript
	vars := map[string]any{"amount": 100}
	updatedVars, err := svc.ExecuteScript(ctx, "setVar('total', amount + 10)", "javascript", vars)
	if err != nil {
		t.Fatalf("ExecuteScript failed: %v", err)
	}
	if fmt.Sprintf("%v", updatedVars["total"]) != "110" {
		t.Errorf("expected total 110, got %v", updatedVars["total"])
	}

	// Test ExecuteConnector (using http-json which is bootstrapped)
	// We'll mock the executor for this test to avoid external calls if needed,
	// but since it's a unit test of the service layer, we can just check if it routes correctly.
	// The http-json connector is added in bootstrapDefaultConnectors.
}
