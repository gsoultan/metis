package bpmn_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	handlersimpl "github.com/gsoultan/gobpm/server/domains/handlers/impl"
	"github.com/gsoultan/gobpm/server/domains/observers/impl"
	"github.com/gsoultan/gobpm/server/domains/services"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	service_impl2 "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
)

// engineHarness wires the engine the same way bpmn_events_test.go does,
// but also hands back the concrete *Engine so a test can drive the inbound
// signal/message path (BroadcastSignal / SendMessage) directly — that is the
// entry point messaging.go's AMQP consumer uses, so it is the real delivery path.
type engineHarness struct {
	svc    services.ServiceFacade
	engine *service_impl2.Engine
	repo   repositories.Repository
	jobSvc servicecontracts.JobService
	projID uuid.UUID
}

func newEngineHarness(t *testing.T, projectName string) engineHarness {
	t.Helper()

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

	handlerFactory := handlersimpl.NewNodeHandlerFactory(engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc, repo.Subscription())
	engine.Apply(
		service_impl2.WithHandlerFactory(handlerFactory),
		service_impl2.WithJobService(jobSvc),
	)

	messagingSvc := service_impl2.NewMessagingService(engine, externalTaskSvc)
	adHocActivator := service_impl2.NewAdHocActivator(engine)
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
		AdHocActivator:       adHocActivator,
		UserService:          userSvc,
		SetupService:         setupSvc,
	})

	org, err := svc.CreateOrganization(ctx, "Test Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	proj, err := svc.CreateProject(ctx, org.ID, projectName, "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	return engineHarness{svc: svc, engine: engine, repo: repo, jobSvc: jobSvc, projID: proj.ID}
}

// taskIsOpen reports whether a task is still work someone could pick up.
//
// ListTasks returns every row regardless of status, so a completed or cancelled
// task would otherwise read as "the process is waiting here".
func taskIsOpen(status entities.TaskStatus) bool {
	switch status {
	case entities.TaskUnclaimed, entities.TaskClaimed, entities.TaskDelegated:
		return true
	default:
		return false
	}
}

// waitingAt reports whether the instance currently has an open task at nodeID.
func (h engineHarness) waitingAt(ctx context.Context, t *testing.T, instanceID uuid.UUID, nodeID string) bool {
	t.Helper()

	tasks, err := h.svc.ListTasks(ctx, h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == nodeID && taskIsOpen(task.Status) {
			return true
		}
	}
	return false
}

// TestSignalCatchEventResumesWaitingInstance is the baseline: a signal carries no
// correlation key, so broadcast delivery should reach every instance parked on
// that signal name.
func TestSignalCatchEventResumesWaitingInstance(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Signal Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "signal-waiter",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "await-approval", Type: entities.IntermediateCatchEvent, Properties: map[string]any{
				"signal_name": "OrderApproved",
			}},
			{ID: "ship", Type: entities.UserTask, Name: "Ship Order"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "await-approval"},
			{ID: "f2", SourceRef: "await-approval", TargetRef: "ship"},
			{ID: "f3", SourceRef: "ship", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "signal-waiter", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	if h.waitingAt(ctx, t, instanceID, "ship") {
		t.Fatal("instance advanced past the catch event before the signal was broadcast")
	}

	if err := h.engine.BroadcastSignal(ctx, h.projID, "OrderApproved", map[string]any{"approvedBy": "auditor"}); err != nil {
		t.Fatalf("broadcast signal: %v", err)
	}

	if !h.waitingAt(ctx, t, instanceID, "ship") {
		t.Error("instance did not resume at \"ship\" after the OrderApproved signal was broadcast")
	}
}

// TestMessageCorrelationRoutesToTheCorrelatedInstance pins the defining property
// of a BPMN message: unlike a signal, it is point-to-point. Two instances of the
// same definition wait on the same message name, distinguished only by the
// correlation key drawn from their own variables. A message correlated to one
// must resume that one and leave the other parked.
func TestMessageCorrelationRoutesToTheCorrelatedInstance(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Message Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "order-payment",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "await-payment", Type: entities.IntermediateCatchEvent, Properties: map[string]any{
				"message_name": "PaymentReceived",
				// The correlation key templates the variable that identifies this
				// conversation; each instance correlates on its own orderId.
				"correlation_key": "${orderId}",
			}},
			{ID: "confirm", Type: entities.UserTask, Name: "Confirm Order"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "await-payment"},
			{ID: "f2", SourceRef: "await-payment", TargetRef: "confirm"},
			{ID: "f3", SourceRef: "confirm", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	firstID, err := h.svc.StartProcess(ctx, h.projID, "order-payment", map[string]any{"orderId": "order-1"})
	if err != nil {
		t.Fatalf("start first instance: %v", err)
	}
	secondID, err := h.svc.StartProcess(ctx, h.projID, "order-payment", map[string]any{"orderId": "order-2"})
	if err != nil {
		t.Fatalf("start second instance: %v", err)
	}

	// The payment for order-1 arrives — this is exactly the call the inbound AMQP
	// consumer makes after reading correlation_key off the delivery payload.
	if err := h.engine.SendMessage(ctx, h.projID, "PaymentReceived", "order-1", map[string]any{"amount": 4200}); err != nil {
		t.Fatalf("send message: %v", err)
	}

	if !h.waitingAt(ctx, t, firstID, "confirm") {
		t.Error("order-1 did not resume: a message correlated to it was accepted but never delivered")
	}
	if h.waitingAt(ctx, t, secondID, "confirm") {
		t.Error("order-2 resumed on a message correlated to order-1")
	}
}

// A signal is a broadcast: every process waiting on it should be told, and one
// subscriber that fails must not silence the others.
//
// The failing subscriber is started first so it is encountered first in the
// subscription list — that is the ordering under which the abort-on-first-error
// behaviour actually loses deliveries.
func TestSignalBroadcastReachesEverySubscriberDespiteOneFailing(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Broadcast Resilience Project")

	failing := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "signal-subscriber-failing",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "wait", Type: entities.IntermediateCatchEvent, Properties: map[string]any{"signal_name": "DayClosed"}},
			{ID: "boom", Type: entities.ScriptTask, Script: `throw new Error("this subscriber is broken");`},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "wait"},
			{ID: "f2", SourceRef: "wait", TargetRef: "boom"},
			{ID: "f3", SourceRef: "boom", TargetRef: "end"},
		},
	}
	healthy := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "signal-subscriber-healthy",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "wait", Type: entities.IntermediateCatchEvent, Properties: map[string]any{"signal_name": "DayClosed"}},
			{ID: "reconcile", Type: entities.UserTask, Name: "Reconcile the day"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "wait"},
			{ID: "f2", SourceRef: "wait", TargetRef: "reconcile"},
			{ID: "f3", SourceRef: "reconcile", TargetRef: "end"},
		},
	}
	for _, def := range []*entities.ProcessDefinition{&failing, &healthy} {
		if _, err := h.svc.CreateDefinition(ctx, def); err != nil {
			t.Fatalf("create definition %s: %v", def.Key, err)
		}
	}

	if _, err := h.svc.StartProcess(ctx, h.projID, "signal-subscriber-failing", nil); err != nil {
		t.Fatalf("start the failing subscriber: %v", err)
	}
	healthyID, err := h.svc.StartProcess(ctx, h.projID, "signal-subscriber-healthy", nil)
	if err != nil {
		t.Fatalf("start the healthy subscriber: %v", err)
	}

	// The broadcast is expected to report the broken subscriber.
	if err := h.engine.BroadcastSignal(ctx, h.projID, "DayClosed", nil); err == nil {
		t.Error("a subscriber failed but the broadcast reported success")
	}

	if !h.waitingAt(ctx, t, healthyID, "reconcile") {
		t.Error("a healthy subscriber never received the signal because another subscriber failed first")
	}
}
