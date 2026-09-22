package instancemigration

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

// Deciding work rather than moving it.
//
// A node mapping can only answer "where does this work go". The question
// management actually asks when it deletes an approval is whether the approval
// that was pending counts as given, as void, or as still owed — and answering
// the first of those with a mapping means pointing the work at some other step,
// which is how somebody else's approval gets performed by the wrong person.

// quotationV1 is the process in the case this was written for:
// submit → supervisor review → operations approve → sales approve → end.
func quotationV1(f *fixture) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: f.project},
		Key:     "quotation",
		Name:    "Quotation approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"q1"}},
			{ID: "supervisorReview", Name: "Supervisor review", Type: entities.UserTask, Assignee: "sam", Incoming: []string{"q1"}, Outgoing: []string{"q2"}},
			{ID: "opsApprove", Name: "Operations approve", Type: entities.UserTask, Assignee: "ollie", Incoming: []string{"q2"}, Outgoing: []string{"q3"}},
			{ID: "salesApprove", Name: "Sales approve", Type: entities.UserTask, Assignee: "sasha", Incoming: []string{"q3"}, Outgoing: []string{"q4"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"q4"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "q1", SourceRef: "start", TargetRef: "supervisorReview"},
			{ID: "q2", SourceRef: "supervisorReview", TargetRef: "opsApprove"},
			{ID: "q3", SourceRef: "opsApprove", TargetRef: "salesApprove"},
			{ID: "q4", SourceRef: "salesApprove", TargetRef: "end"},
		},
	}
}

// quotationV2 is management's change: the operations approval is gone.
func quotationV2(f *fixture) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: f.project},
		Key:     "quotation",
		Name:    "Quotation approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"q1"}},
			{ID: "supervisorReview", Name: "Supervisor review", Type: entities.UserTask, Assignee: "sam", Incoming: []string{"q1"}, Outgoing: []string{"q2"}},
			{ID: "salesApprove", Name: "Sales approve", Type: entities.UserTask, Assignee: "sasha", Incoming: []string{"q2"}, Outgoing: []string{"q4"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"q4"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "q1", SourceRef: "start", TargetRef: "supervisorReview"},
			{ID: "q2", SourceRef: "supervisorReview", TargetRef: "salesApprove"},
			{ID: "q4", SourceRef: "salesApprove", TargetRef: "end"},
		},
	}
}

// parkedOnOpsApprove starts a quotation and walks it to the operations
// manager's inbox — the population the whole question is about.
func (f *fixture) parkedOnOpsApprove(t *testing.T) (v1, v2 string) {
	t.Helper()
	first, err := f.svc.CreateDefinition(f.ctx, quotationV1(f))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	if _, err := f.svc.StartProcess(f.ctx, f.project, "quotation", nil); err != nil {
		t.Fatalf("start a quotation: %v", err)
	}
	f.completeTaskOn(t, "supervisorReview", "sam")

	second, err := f.svc.CreateDefinition(f.ctx, quotationV2(f))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	return first.String(), second.String()
}

// TestSkippingAPendingApprovalAdvancesToTheNextStep is B2 — "the approval is
// moot" — and it is the answer a node mapping could not express.
//
// Mapping opsApprove onto salesApprove was the only way to say this before, and
// it did not mean it: it moved the operations manager's task onto the sales
// manager's step instead of recording that the operations approval never
// happened.
func TestSkippingAPendingApprovalAdvancesToTheNextStep(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)

	actions := map[string]servicecontracts.NodeAction{
		"opsApprove": {Kind: servicecontracts.NodeActionSkip, Reason: "the operations manager role was eliminated"},
	}
	opts := []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(actions),
		servicecontracts.WithActor("dita"),
	}

	if err := f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil, opts...); err != nil {
		t.Fatalf("apply: %v", err)
	}

	// The operations task is gone and the sales manager has theirs.
	tasks := f.openTasks(t)
	if len(tasks) != 1 {
		t.Fatalf("expected one open task after the skip, found %d", len(tasks))
	}
	if tasks[0].NodeID() != "salesApprove" {
		t.Fatalf("the open task is on %q; skipping the operations approval should advance to the sales approval", tasks[0].NodeID())
	}
	if tasks[0].Assignee == nil || tasks[0].Assignee.Username != "sasha" {
		got := "<nobody>"
		if tasks[0].Assignee != nil {
			got = tasks[0].Assignee.Username
		}
		t.Fatalf("the sales approval is assigned to %q; the operations manager must not inherit it", got)
	}

	instance := f.onlyInstance(t)
	if instance.Status != entities.ProcessActive {
		t.Fatalf("the instance is %q; it still has a sales approval to do", instance.Status)
	}
	if instance.Definition == nil || instance.Definition.ID.String() != v2 {
		t.Fatal("the instance was not moved onto version 2")
	}
}

// TestASkippedStepSaysSoOnTheTrail. A skipped approval that reads like an
// approval somebody gave is the one outcome this must never produce.
func TestASkippedStepSaysSoOnTheTrail(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)

	opts := []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionSkip, Reason: "the operations manager role was eliminated"},
		}),
		servicecontracts.WithActor("dita"),
	}
	if err := f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil, opts...); err != nil {
		t.Fatalf("apply: %v", err)
	}

	entries, err := f.svc.GetAuditLogs(f.ctx, f.onlyInstance(t).ID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	for _, entry := range entries {
		if entry.Type != impl.EventNodeSkipped {
			continue
		}
		for _, want := range []string{"opsApprove", "dita", "role was eliminated"} {
			if !strings.Contains(entry.Narrative, want) {
				t.Errorf("the trail entry does not mention %q: %q", want, entry.Narrative)
			}
		}
		return
	}
	t.Fatalf("an approval was skipped and the trail has no %q entry", impl.EventNodeSkipped)
}

// TestCancellingEndsTheInstanceAndLeavesItOnItsOwnVersion is B3 — "the approval
// was void and so is what it was approving".
func TestCancellingEndsTheInstanceAndLeavesItOnItsOwnVersion(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)

	opts := []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionCancel, Reason: "the step should never have been there; re-quote"},
		}),
		servicecontracts.WithActor("dita"),
	}
	if err := f.svc.MigrateInstances(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil, opts...); err != nil {
		t.Fatalf("apply: %v", err)
	}

	instance := f.onlyInstance(t)
	if instance.Status != entities.ProcessCancelled {
		t.Fatalf("the instance is %q; an instance somebody called off is not one that completed", instance.Status)
	}
	if len(instance.Tokens) != 0 {
		t.Fatalf("a cancelled instance still holds %d token(s)", len(instance.Tokens))
	}
	// It ran on version 1 and that is what its record should say.
	if instance.Definition == nil || instance.Definition.ID.String() != v1 {
		t.Fatal("a cancelled instance was moved onto version 2; it will never run again, so its record should name the version it ran")
	}
	if open := f.openTasks(t); len(open) != 0 {
		t.Fatalf("%d task(s) are still open on a cancelled instance", len(open))
	}
}

// TestADecisionWithoutAReasonIsRefused. The reason is the entire value of the
// record: without it the trail cannot tell a step nobody performed from a step
// somebody did.
func TestADecisionWithoutAReasonIsRefused(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)

	plan, err := f.svc.PlanInstanceMigration(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil,
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionSkip},
		}))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Applicable() {
		t.Fatal("a skip with no reason was accepted")
	}
}

// TestANodeCannotBeBothMappedAndSkipped: two contradictory instructions for one
// node, and guessing which was meant is how the wrong one gets applied.
func TestANodeCannotBeBothMappedAndSkipped(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)

	plan, err := f.svc.PlanInstanceMigration(f.ctx, uuidOf(t, v1), uuidOf(t, v2),
		map[string]string{"opsApprove": "salesApprove"},
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionSkip, Reason: "moot"},
		}))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Applicable() {
		t.Fatal("a node that was both mapped and skipped was accepted")
	}
}

// TestSkippingAGatewayIsRefused. Skipping means following the node's one
// outgoing flow; a gateway has several, and choosing between them is a business
// decision the migration has no basis for making.
func TestSkippingAGatewayIsRefused(t *testing.T) {
	f := newFixture(t)
	v1 := f.deployParallel(t, "join")
	if _, err := f.svc.StartProcess(f.ctx, f.project, "parallel-approval", nil); err != nil {
		t.Fatalf("start an instance: %v", err)
	}
	v2 := f.deployParallel(t, "gate")

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, map[string]string{"join": "gate"},
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"split": {Kind: servicecontracts.NodeActionSkip, Reason: "no"},
		}))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Applicable() {
		t.Fatal("skipping a parallel gateway was accepted; which branch would it have taken?")
	}
}

// TestThePlanReportsTheDecisions. "Two instances move" and "two instances have
// an approval skipped" must not read identically in a preview.
func TestThePlanReportsTheDecisions(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)

	plan, err := f.svc.PlanInstanceMigration(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil,
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionSkip, Reason: "role eliminated"},
		}))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !plan.Applicable() {
		t.Fatalf("a well-formed skip was refused: %v", plan.Refusals)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("the plan reports %d actions; one node is being skipped", len(plan.Actions))
	}
	if plan.Actions[0].NodeID != "opsApprove" || plan.Actions[0].Kind != "skip" {
		t.Fatalf("the plan reports the wrong action: %+v", plan.Actions[0])
	}
	if plan.Actions[0].Reason != "role eliminated" {
		t.Errorf("the plan drops the reason, which is what the trail needs: %+v", plan.Actions[0])
	}
}

// TestWorkOnASkippedNodeNeedsNowhereToLand. Without this the skip is
// unreachable: the planner would refuse the migration for stranding the very
// task the caller asked it to cancel.
func TestWorkOnASkippedNodeNeedsNowhereToLand(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)

	// No mapping at all: v2 has no "opsApprove", and that is the point.
	plan, err := f.svc.PlanInstanceMigration(f.ctx, uuidOf(t, v1), uuidOf(t, v2), nil,
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionSkip, Reason: "role eliminated"},
		}))
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !plan.Applicable() {
		t.Fatalf("work that is being skipped was refused for having nowhere to land: %v", plan.Refusals)
	}
}

// uuidOf parses an id the fixture handed back as a string.
func uuidOf(t *testing.T, id string) uuid.UUID {
	t.Helper()
	parsed, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("not an id: %v", err)
	}
	return parsed
}

// openTasks is the work still in somebody's hands. ListTasks returns every
// status, so a completed step and a cancelled one both come back with it.
func (f *fixture) openTasks(t *testing.T) []entities.Task {
	t.Helper()
	all, err := f.svc.ListTasks(f.ctx, f.project)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	var open []entities.Task
	for _, task := range all {
		switch task.Status {
		case entities.TaskUnclaimed, entities.TaskClaimed, entities.TaskDelegated:
			open = append(open, task)
		}
	}
	return open
}
