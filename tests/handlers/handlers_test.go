package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gsoultan/gobpm/server/domains/entities"
	handlersimpl "github.com/gsoultan/gobpm/server/domains/handlers/impl"
	"github.com/gsoultan/gobpm/server/domains/observers/impl"
	"github.com/gsoultan/gobpm/server/domains/services"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	service_impl2 "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
)

func TestInclusiveGateway(t *testing.T) {
	ctx := t.Context()
	svc, _ := newHandlerHarness(t)

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
	svc, jobSvc := newHandlerHarness(t)

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

	if _, err := svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	// A timer defers; it does not block whoever started the process. This used
	// to assert the opposite — that StartProcess took at least the timer's
	// duration — because the job service was replaced by a double that slept in
	// the caller. A three-day timer would have held an HTTP request open for
	// three days.
	start := time.Now()
	instanceID, err := svc.StartProcess(ctx, proj.ID, "timer-process", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("starting the process waited %v for a 100ms timer; it should return at once", elapsed)
	}

	instance, err := svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if instance.Status == "completed" {
		t.Fatal("the process finished before the timer was due")
	}

	// Once it is due, the worker picks it up and the process carries on.
	time.Sleep(150 * time.Millisecond)
	if err := jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	instance, err = svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	if instance.Status != "completed" {
		t.Errorf("after the timer fired the instance is %q, want completed", instance.Status)
	}
}

// A service task calls something, and the process carries on with what came
// back.
//
// This used to assert that the instance gained a "service_completed" variable —
// which no part of the engine writes. It came from the test double, which
// marked a node done by inventing that variable, so a test named for service
// tasks proved only that the double had run.
func TestServiceTask(t *testing.T) {
	// The task URL comes from a definition, which is user-authored, so the HTTP
	// client refuses loopback unless told otherwise. The stub below is on
	// 127.0.0.1, so this test opts in for itself.
	t.Setenv("GOBPM_HTTP_ALLOW_PRIVATE_NETWORKS", "true")

	var called bool
	var received map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"reference": "REF-4417"})
	}))
	defer api.Close()

	ctx := t.Context()
	svc, jobSvc := newHandlerHarness(t)

	org, _ := svc.CreateOrganization(ctx, "Test Org", "")
	proj, _ := svc.CreateProject(ctx, org.ID, "Service Project", "")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: proj.ID},
		Key:     "service-process",
		Name:    "Service Process",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "service", Type: entities.ServiceTask, Name: "My Service", Properties: map[string]any{
				"http_url":         api.URL,
				"http_method":      "POST",
				"input_orderId":    "order_id",
				"output_reference": "bookingReference",
			}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "service"},
			{ID: "f2", SourceRef: "service", TargetRef: "end"},
		},
	}
	if _, err := svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := svc.StartProcess(ctx, proj.ID, "service-process", map[string]any{"orderId": "A-1"})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	// The work is queued, so nothing has been called yet.
	if called {
		t.Error("the endpoint was called before the job ran")
	}
	if err := jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if !called {
		t.Fatal("the service task did not call anything")
	}
	if received["order_id"] != "A-1" {
		t.Errorf("the endpoint received order_id=%v, want \"A-1\" — the input mapping did not rename it", received["order_id"])
	}

	instance, err := svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if instance.Variables["bookingReference"] != "REF-4417" {
		t.Errorf("bookingReference = %v, want REF-4417 — the response did not come back", instance.Variables["bookingReference"])
	}
	if instance.Status != "completed" {
		t.Errorf("instance is %q, want completed once the call returned", instance.Status)
	}
}

func TestAdvancedTasks(t *testing.T) {
	ctx := t.Context()
	svc, _ := newHandlerHarness(t)

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

// newHandlerHarness wires the service facade the way the server does, with the
// real job service.
//
// These tests used testutils.SynchronousJobService, which runs a service task
// by writing "<node>_completed" into the variables and moving on, and runs a
// timer by sleeping in the caller. Neither is what the engine does, so the
// tests asserted the double's behaviour rather than the product's — and that is
// how three connector bugs shipped with this suite green.
func newHandlerHarness(t *testing.T) (services.ServiceFacade, servicecontracts.JobService) {
	t.Helper()
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	dispatcher := impl.NewEventDispatcher()

	engine := service_impl2.NewExecutionEngine(repo, dispatcher)
	connectorSvc := service_impl2.NewConnectorService(repo)
	taskSvc := service_impl2.NewTaskService(repo, engine, service_impl2.NewAuditWriter(repo.Audit()))
	externalTaskSvc := service_impl2.NewExternalTaskService(repo, engine)
	decisionSvc := service_impl2.NewDecisionService(repo, service_impl2.NewDecisionTableEvaluator(service_impl2.NewFEELEvaluator()))
	jobSvc := service_impl2.NewJobService(repo, engine, connectorSvc,
		service_impl2.NewNoOpLocker(), handlersimpl.NewErrorBoundaryMatcher())
	sse := impl.NewSSEObserver()

	engine.Apply(
		service_impl2.WithHandlerFactory(handlersimpl.NewNodeHandlerFactory(
			engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc,
			repo.Subscription(),
		)),
		service_impl2.WithJobService(jobSvc),
	)

	svc := services.NewService(services.ServiceParams{
		OrganizationService:  service_impl2.NewOrganizationService(repo),
		ProjectService:       service_impl2.NewProjectService(repo),
		DefinitionService:    service_impl2.NewDefinitionService(repo),
		TaskService:          taskSvc,
		ExecutionEngine:      engine,
		JobService:           jobSvc,
		ExternalTaskService:  externalTaskSvc,
		DecisionService:      decisionSvc,
		MigrationService:     service_impl2.NewMigrationService(repo),
		ConnectorService:     connectorSvc,
		CollaborationService: service_impl2.NewCollaborationService(sse),
		MessagingService:     service_impl2.NewMessagingService(engine, externalTaskSvc),
		UserService:          service_impl2.NewUserService(repo, "test-jwt-secret"),
		SetupService:         service_impl2.NewSetupService(nil),
	})
	return svc, jobSvc
}
