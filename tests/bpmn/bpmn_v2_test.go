package bpmn_test

import (
	"testing"

	"github.com/gsoultan/gobpm/server/domains/entities"
	handlersimpl "github.com/gsoultan/gobpm/server/domains/handlers/impl"
	"github.com/gsoultan/gobpm/server/domains/observers/impl"
	service_impl2 "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
)

func TestTerminateEndEvent(t *testing.T) {
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

	handlerFactory := handlersimpl.NewNodeHandlerFactory(engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc, repo.Subscription())
	engine.Apply(
		service_impl2.WithHandlerFactory(handlerFactory),
		service_impl2.WithJobService(jobSvc),
	)

	org, _ := orgSvc.CreateOrganization(ctx, "Test Org", "")
	proj, _ := projectSvc.CreateProject(ctx, org.ID, "Test Project", "")

	// Define process: Start -> Parallel -> (Task1, Terminate)
	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: proj.ID},
		Key:     "terminate-process",
		Name:    "Terminate Process",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "parallel", Type: entities.ParallelGateway},
			{ID: "task1", Type: entities.UserTask, Name: "User Task"},
			{ID: "terminate", Type: entities.TerminateEndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "parallel"},
			{ID: "f2", SourceRef: "parallel", TargetRef: "task1"},
			{ID: "f3", SourceRef: "parallel", TargetRef: "terminate"},
		},
	}

	_, _ = defSvc.CreateDefinition(ctx, &def)

	instanceID, err := engine.StartProcess(ctx, proj.ID, "terminate-process", nil)
	if err != nil {
		t.Fatalf("failed to start process: %v", err)
	}

	instance, err := engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("failed to get instance: %v", err)
	}

	// Process should be completed because TerminateEndEvent was reached
	if instance.Status != entities.ProcessCompleted {
		t.Errorf("expected process status completed, got %v", instance.Status)
	}

	if len(instance.Tokens) != 0 {
		t.Errorf("expected 0 tokens after termination, got %d", len(instance.Tokens))
	}
}

func TestEventBasedGateway(t *testing.T) {
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

	handlerFactory := handlersimpl.NewNodeHandlerFactory(engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc, repo.Subscription())
	engine.Apply(
		service_impl2.WithHandlerFactory(handlerFactory),
		service_impl2.WithJobService(jobSvc),
	)

	org, _ := orgSvc.CreateOrganization(ctx, "Test Org", "")
	proj, _ := projectSvc.CreateProject(ctx, org.ID, "Test Project", "")

	// Define process: Start -> EventGateway -> (Catch1, Catch2) -> End
	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: proj.ID},
		Key:     "event-gateway-process",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "gateway", Type: entities.EventBasedGateway},
			{ID: "catch1", Type: entities.IntermediateCatchEvent, Properties: map[string]any{"signal_name": "S1"}},
			{ID: "catch2", Type: entities.IntermediateCatchEvent, Properties: map[string]any{"signal_name": "S2"}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "gateway"},
			{ID: "f2", SourceRef: "gateway", TargetRef: "catch1"},
			{ID: "f3", SourceRef: "gateway", TargetRef: "catch2"},
			{ID: "f4", SourceRef: "catch1", TargetRef: "end"},
			{ID: "f5", SourceRef: "catch2", TargetRef: "end"},
		},
	}

	_, _ = defSvc.CreateDefinition(ctx, &def)

	instanceID, _ := engine.StartProcess(ctx, proj.ID, "event-gateway-process", nil)

	// Check subscriptions
	subs, _ := repo.Subscription().ListByInstance(ctx, instanceID)
	if len(subs) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d", len(subs))
	}

	// Trigger S1
	err := engine.BroadcastSignal(ctx, proj.ID, "S1", nil)
	if err != nil {
		t.Fatalf("failed to broadcast signal: %v", err)
	}

	instance, _ := engine.GetInstance(ctx, instanceID)
	// Process should have followed catch1 path and completed
	if instance.Status != entities.ProcessCompleted {
		t.Errorf("expected process status completed, got %v", instance.Status)
	}

	// Subscriptions for catch2 should be gone
	subs, _ = repo.Subscription().ListByInstance(ctx, instanceID)
	if len(subs) != 0 {
		t.Errorf("expected 0 subscriptions after one event triggered, got %d", len(subs))
	}
}
