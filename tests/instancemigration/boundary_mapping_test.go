package instancemigration

import (
	"slices"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/endpoints/definition"
)

// A boundary event mapped onto a step.
//
// A mapping says where the work on a node goes. The work on a boundary event
// is a timer or a waiting message that, when it fires, acts on the node it
// sits on. On a boundary event that means leaving the step it is attached to
// by the event's own path. Mapped onto a step that is not a boundary event,
// the same timer fires on that step, and the engine reads a timer on a step as
// the step's own wait coming to its end: it advances the instance past it.

// TestABoundaryEventMayNotBeMappedOntoAStep.
//
// Root cause: the planner checked a mapped boundary event only when its target
// was a boundary event too; any other target was accepted because it exists.
//
// An approval with a three-day deadline. The new version keeps the approval
// and drops the deadline, and the deadline is mapped onto the approval — what
// "map each one to a node it does have" invites. Before: the plan was
// applicable, the deadline's timer was re-pointed at the approval, and when
// the three days passed the approval was recorded as performed and the
// instance finished, with the approver's task still open on it.
func TestABoundaryEventMayNotBeMappedOntoAStep(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, deadlined(f.project, true), deadlined(f.project, false))
	instance := f.onlyInstance(t)
	request := func(dryRun bool) definition.MigrateInstancesRequest {
		return definition.MigrateInstancesRequest{
			SourceDefinitionID: v1.String(), TargetDefinitionID: v2.String(), DryRun: &dryRun,
			NodeMapping: map[string]string{"deadline": "approve"},
		}
	}

	// The dry run refuses, naming the event and the step as people know them.
	_, body := f.migrateOverTheEndpoint(t, request(true))
	plan := planOf(t, body)
	refusals := strings.Join(plan.Refusals, "; ")
	t.Logf("refused, and told: %s", refusals)
	if plan.Applicable() {
		t.Fatal("a deadline mapped onto the approval it is attached to was reported as applicable")
	}
	for _, want := range []string{"Three days passed", "Approve", "boundary event", "only to a boundary event"} {
		if !strings.Contains(refusals, want) {
			t.Errorf("the refusal does not say %q: %s", want, refusals)
		}
	}

	// And the apply moves nothing.
	reply, err := definition.MakeMigrateInstancesEndpoint(f.svc)(f.ctx, request(false))
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	answered := reply.(definition.MigrateInstancesResponse)
	if answered.Err == nil || answered.Applied || !strings.Contains(answered.Err.Error(), "only to a boundary event") {
		t.Fatalf("the apply should have been refused for the mapping: applied=%v err=%v", answered.Applied, answered.Err)
	}
	f.assertWaitingAt(t, v1, "approve")
	f.assertNoMigrationEntries(t, instance.ID)
	if rows := f.jobRows(t, instance.ID); len(rows) != 1 || !strings.Contains(rows[0], "node=deadline ") {
		t.Errorf("the deadline's timer should still be on the deadline: %v", rows)
	}
	f.assertNothingIsStranded(t)

	// The three days pass, and the deadline does what the version it runs
	// says: the approval is withdrawn and the escalation opens.
	if err := f.db.WithContext(f.ctx).Exec(
		`UPDATE jobs SET next_run_at = now() - interval '1 minute' WHERE instance_id = ?`, instance.ID).Error; err != nil {
		t.Fatalf("bring the deadline due: %v", err)
	}
	if err := f.svc.ProcessPendingJobs(f.ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}
	f.assertWaitingAt(t, v1, "escalate")
	f.runsToItsEnd(t)
}

// The refusal for a boundary event's work used to end "map each one to a node
// it does have", which for a boundary event the new version dropped is the
// mapping refused above. It now also says what does work, and that does.
func TestTheRefusalForABoundaryEventsWorkSaysWhatWorks(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, deadlined(f.project, true), deadlined(f.project, false))
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	refusals := strings.Join(plan.Refusals, "; ")
	t.Logf("refused, and told: %s", refusals)
	for _, want := range []string{
		"nowhere to put the work parked on deadline; map each one to a node it does have",
		`"Three days passed" is a boundary event`, "only to a boundary event", "name the event in the same decision",
	} {
		if !strings.Contains(refusals, want) {
			t.Errorf("the refusal does not say %q: %s", want, refusals)
		}
	}

	// Doing what it says is accepted: the approval held, the deadline named
	// with it.
	hold := servicecontracts.NodeAction{Kind: servicecontracts.NodeActionHold, Reason: "the deadline was dropped; look at each"}
	advised, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil,
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{"approve": hold, "deadline": hold}))
	if err != nil {
		t.Fatalf("plan as advised: %v", err)
	}
	if !advised.Applicable() {
		t.Fatalf("what the refusal advises was refused: %v", advised.Refusals)
	}
	f.assertNothingIsStranded(t)
}

// What an operator does instead, when the new version keeps a step and drops
// the deadline on it: the instances waiting at the step are held there, the
// deadline named beside the step, and once one has left the step the same
// migration run again moves it. The deadline's timer, still pending, is let
// stand because the deadline is named with its step, and is dismissed when it
// comes due.
func TestAnInstanceAtAStepWhoseDeadlineWasDroppedIsHeldAndMovedOnceItHasLeftTheStep(t *testing.T) {
	f := newFixture(t)
	without := approvalWithDeadline(f.project, true)
	without.Nodes = slices.DeleteFunc(without.Nodes, func(node *entities.Node) bool {
		return node.ID == "deadline" || node.ID == "escalate" || node.ID == "escalated"
	})
	without.Flows = slices.DeleteFunc(without.Flows, func(flow *entities.SequenceFlow) bool {
		return flow.ID == "p4" || flow.ID == "p5"
	})
	v1, v2 := f.startedOn(t, approvalWithDeadline(f.project, true), without)
	hold := servicecontracts.NodeAction{Kind: servicecontracts.NodeActionHold, Reason: "the deadline was dropped; look at each"}
	held := []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{"approve": hold, "deadline": hold}),
		servicecontracts.WithActor("dita"),
	}

	first, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, held...)
	if err != nil {
		t.Fatalf("the first run: %v", err)
	}
	instance := f.assertWaitingAt(t, v1, "approve")
	if first.Changed != 1 || f.openIncidentsOn(t, instance.ID, "approve") != 1 {
		t.Fatalf("the instance should be held at the approval, on the version it runs: changed=%d", first.Changed)
	}

	// Its holder gives the approval, and the same migration is run again.
	f.completeTaskOn(t, "approve", "ada")
	f.assertWaitingAt(t, v1, "sign")
	second, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, held...)
	if err != nil {
		t.Fatalf("the second run: %v", err)
	}
	if second.Changed != 1 || len(second.PassedOver) != 0 {
		t.Errorf("the second run says it acted on %d and passed over %d (%+v), want one and none", second.Changed, len(second.PassedOver), second.PassedOver)
	}
	f.assertWaitingAt(t, v2, "sign")
	f.assertNothingIsStranded(t)

	// The three days pass: the timer finds no token and is dismissed.
	if err := f.db.WithContext(f.ctx).Exec(
		`UPDATE jobs SET next_run_at = now() - interval '1 minute' WHERE instance_id = ?`, instance.ID).Error; err != nil {
		t.Fatalf("bring the deadline due: %v", err)
	}
	if err := f.svc.ProcessPendingJobs(f.ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}
	f.assertWaitingAt(t, v2, "sign")
	if rows := f.jobRows(t, instance.ID); len(rows) != 1 || !strings.HasSuffix(rows[0], "status=completed") {
		t.Errorf("the deadline's timer should have been dismissed when it came due: %v", rows)
	}
	f.runsToItsEnd(t)
}
