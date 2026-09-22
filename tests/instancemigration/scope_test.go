package instancemigration

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// waitingOnMessage is start → wait (intermediate message catch) → done → end.
//
// The catch event is what keeps a subscription: a promise that a message
// arriving later will find this instance waiting for it.
func waitingOnMessage(projectID uuid.UUID, waitID string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "awaiting-reply",
		Name:    "Awaiting reply",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"w1"}},
			{
				ID: waitID, Name: "Wait for the ERP", Type: entities.IntermediateCatchEvent,
				Properties: map[string]any{"message_name": "erp-replied"},
				Incoming:   []string{"w1"}, Outgoing: []string{"w2"},
			},
			{ID: "done", Name: "Done", Type: entities.UserTask, Assignee: "ada", Incoming: []string{"w2"}, Outgoing: []string{"w3"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"w3"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "w1", SourceRef: "start", TargetRef: waitID},
			{ID: "w2", SourceRef: waitID, TargetRef: "done"},
			{ID: "w3", SourceRef: "done", TargetRef: "end"},
		},
	}
}

// TestAWaitingSubscriptionMovesWithTheWorkItBelongsTo.
//
// Root cause: migration wrote tokens, tasks and jobs. A subscription is a
// fourth row keyed by node id, so a renamed catch event left it naming a node
// the new version does not have — and the message, when it arrived, correlated
// to nothing and the sender was never told.
func TestAWaitingSubscriptionMovesWithTheWorkItBelongsTo(t *testing.T) {
	f := newFixture(t)

	v1, err := f.svc.CreateDefinition(f.ctx, waitingOnMessage(f.project, "wait"))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "awaiting-reply", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	v2, err := f.svc.CreateDefinition(f.ctx, waitingOnMessage(f.project, "awaitErp"))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	if err := f.svc.MigrateInstances(f.ctx, v1, v2, map[string]string{"wait": "awaitErp"}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// The proof is behavioural: the message still finds the instance and moves
	// it on to the step after the wait.
	if err := f.svc.SendMessage(f.ctx, f.project, "erp-replied", "", nil); err != nil {
		t.Fatalf("send the message: %v", err)
	}
	tasks := f.openTasks(t)
	if len(tasks) != 1 || tasks[0].NodeID() != "done" {
		t.Fatalf("the message did not reach the migrated instance; open tasks: %d", len(tasks))
	}
}

// TestSkippingAWaitDoesNotLeaveItListening.
//
// The engine's own advance is what releases the subscription here, which is
// exactly why skip is built on Proceed rather than on a hand-written token
// move: a subscription is one of several things that have to be let go when a
// node is left behind, and the engine already knows all of them. This test is
// the regression guard on that staying true.
func TestSkippingAWaitDoesNotLeaveItListening(t *testing.T) {
	f := newFixture(t)

	v1, err := f.svc.CreateDefinition(f.ctx, waitingOnMessage(f.project, "wait"))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "awaiting-reply", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	v2, err := f.svc.CreateDefinition(f.ctx, waitingOnMessage(f.project, "awaitErp"))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	// Skip the wait instead of mapping it: the instance advances past it, so
	// the subscription it was holding must not survive.
	opts := []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"wait": {Kind: servicecontracts.NodeActionSkip, Reason: "the ERP integration was retired"},
		}),
		servicecontracts.WithActor("dita"),
	}
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, nil, opts...); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// Already advanced to "done" by the skip.
	if tasks := f.openTasks(t); len(tasks) != 1 || tasks[0].NodeID() != "done" {
		t.Fatalf("the skip did not advance past the wait; open tasks: %d", len(tasks))
	}
	// And a late message must not move it a second time.
	if err := f.svc.SendMessage(f.ctx, f.project, "erp-replied", "", nil); err != nil {
		t.Fatalf("send the message: %v", err)
	}
	if tasks := f.openTasks(t); len(tasks) != 1 || tasks[0].NodeID() != "done" {
		t.Fatalf("a late message moved an instance that had already advanced past the wait; open tasks: %d", len(tasks))
	}
}

// TestHoldingAnInstanceLeavesItAloneAndRaisesAnIncident is the answer for the
// instances nobody can decide in bulk.
func TestHoldingAnInstanceLeavesItAloneAndRaisesAnIncident(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)

	opts := []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionHold, Reason: "customer is mid-negotiation; ask the account manager"},
		}),
		servicecontracts.WithActor("dita"),
	}
	if err := f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil, opts...); err != nil {
		t.Fatalf("apply: %v", err)
	}

	instance := f.onlyInstance(t)
	// Nothing moved: that is the whole point of a hold.
	if instance.Status != entities.ProcessActive {
		t.Fatalf("a held instance is %q; a hold changes where it is visible, not what it is", instance.Status)
	}
	if instance.Definition == nil || instance.Definition.ID.String() != v1 {
		t.Fatal("a held instance was migrated anyway")
	}
	if tasks := f.openTasks(t); len(tasks) != 1 || tasks[0].NodeID() != "opsApprove" {
		t.Fatalf("a hold disturbed the work parked on the node; open tasks: %+v", tasks)
	}

	incidents, err := f.svc.ListIncidents(f.ctx, instance.ID)
	if err != nil {
		t.Fatalf("list incidents: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("a held instance raised %d incidents; it should raise exactly one", len(incidents))
	}
}

// TestAMigrationCanBeNarrowedToNamedInstances. One mapping was one policy for
// everybody; the populations a removed step creates are rarely one decision.
func TestAMigrationCanBeNarrowedToNamedInstances(t *testing.T) {
	f := newFixture(t)

	first, err := f.svc.CreateDefinition(f.ctx, approval(f.project, "approve"))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	moveMe, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil)
	if err != nil {
		t.Fatalf("start the first instance: %v", err)
	}
	leaveMe, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil)
	if err != nil {
		t.Fatalf("start the second instance: %v", err)
	}
	second := f.deploy(t, "review")

	plan, err := f.svc.PlanInstanceMigration(f.ctx, first, second,
		map[string]string{"approve": "review"},
		servicecontracts.WithInstances(moveMe))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Instances != 1 {
		t.Fatalf("the plan covers %d instances; one was named", plan.Instances)
	}

	if err := f.svc.MigrateInstances(f.ctx, first, second,
		map[string]string{"approve": "review"},
		servicecontracts.WithInstances(moveMe)); err != nil {
		t.Fatalf("apply: %v", err)
	}

	instances, err := f.svc.ListInstances(f.ctx, f.project)
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	for _, instance := range instances {
		if instance.Definition == nil {
			continue
		}
		switch instance.ID {
		case moveMe:
			if instance.Definition.ID != second {
				t.Error("the named instance was not migrated")
			}
		case leaveMe:
			if instance.Definition.ID != first {
				t.Error("an instance nobody named was migrated anyway")
			}
		}
	}
}

// TestNamingAnInstanceThatIsNotThereIsRefused. Listing twelve and having eleven
// moved leaves nobody able to find out which one did not.
func TestNamingAnInstanceThatIsNotThereIsRefused(t *testing.T) {
	f := newFixture(t)
	v1 := f.deploy(t, "approve")
	if _, err := f.svc.StartProcess(f.ctx, f.project, "expense-approval", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	v2 := f.deploy(t, "review")

	_, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2,
		map[string]string{"approve": "review"},
		servicecontracts.WithInstances(uuid.New()))
	if err == nil {
		t.Fatal("naming an instance that is not running on the source version was accepted")
	}
}
