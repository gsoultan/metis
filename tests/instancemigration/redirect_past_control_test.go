package instancemigration

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

// A mapping can take an instance past a control without skipping anything and
// without dropping the control from the new version. That is not one
// administrator's call.

// R1. prepare → the control → sign, the same graph in both versions, the
// instance waiting at prepare. A mapping sends prepare to sign: the instance
// lands beyond the control, which nobody ever performs.
//
// Root cause: what needs a second administrator was a skip, or a control the
// new version drops. A redirect drops nothing and skips nothing — the control
// is still in the graph, behind the instance — so the plan held nothing, asked
// nobody, and one administrator moved an instance past a control with
// instance_migrated the only trace.
func TestARedirectPastAControlIsNotOneAdministratorsCall(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, prepared(f.project, true, true), prepared(f.project, true, true))
	instance := f.assertWaitingAt(t, v1, "prepare")
	past := map[string]string{"prepare": "sign"}
	opts := []servicecontracts.MigrationOption{servicecontracts.WithInstances(instance.ID), servicecontracts.WithActor("dita")}

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, past, opts...)
	if err != nil || !plan.Applicable() {
		t.Fatalf("the plan: %+v %v", plan, err)
	}
	if !plan.RequiresSecondApprover || len(plan.SecondApproverReasons) != 1 {
		t.Fatalf("a redirect past a control needs nobody else: requires_second_approver=%v reasons=%v", plan.RequiresSecondApprover, plan.SecondApproverReasons)
	}
	reason := plan.SecondApproverReasons[0]
	for _, want := range []string{"“Prepare”", "“Sign”", "“Second signature”", "may no longer be ahead"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the reason does not say %s: %q", want, reason)
		}
	}
	// It is not a control the migration is known to drop: nothing is held, and
	// nothing has to be acknowledged.
	if len(plan.ComplianceHolds) != 0 {
		t.Fatalf("the plan holds %+v; the control is still in the new version", plan.ComplianceHolds)
	}

	// On one administrator's call it is refused, and nothing moves.
	if _, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, past, opts...); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("the redirect on one administrator's call: %v, want it forbidden", err)
	}
	f.assertWaitingAt(t, v1, "prepare")
	if rows := f.ledger(t, instance.ID); len(rows) != 0 {
		t.Fatalf("the refused redirect left %d ledger row(s)", len(rows))
	}
	f.assertNoMigrationEntries(t, instance.ID)

	// Asked for and approved, it goes through, and the record says who
	// approved it and which step the instance was moved from and to.
	result, err := f.applyWithApproval(t, v1, v2, past, opts...)
	if err != nil || result.Changed != 1 {
		t.Fatalf("the approved redirect: %+v %v", result, err)
	}
	f.assertWaitingAt(t, v2, "sign")
	entry := f.entryOf(t, instance.ID, impl.EventInstanceMigrated)
	if entry.Data["approved_by"] != "omar" || entry.Data["request_id"] != f.underApproval.String() ||
		!strings.Contains(entry.Narrative, "prepare→sign") || !strings.Contains(entry.Narrative, "A second administrator, omar, approved this migration") {
		t.Fatalf("the entry of the approved redirect: %q %v", entry.Narrative, entry.Data)
	}
	if moves, _ := entry.Data["task_moves"].([]any); len(moves) != 1 || moves[0] != "prepare→sign" {
		t.Fatalf("the entry's moves are %v, want the step it was moved from and to", entry.Data["task_moves"])
	}
	// The code cannot know the control was lost — it may still lie ahead —
	// so no row says it was.
	for _, row := range f.ledger(t, instance.ID) {
		if row.Kind == entities.DeviationControlWaived {
			t.Fatalf("the redirect is recorded as a control waived, which nobody knows: %+v", row)
		}
	}
}

// A redirect asks only where there is a control an instance has not passed.
// In a process with none, a mapping alone — a redirect too — still applies on
// one administrator's call.
func TestARedirectInAProcessWithNoControlStillAppliesOnOneCall(t *testing.T) {
	f := newFixture(t)
	first, second := f.parkedOnOpsApprove(t)
	v1, v2 := uuidOf(t, first), uuidOf(t, second)
	redirect := map[string]string{"opsApprove": "salesApprove"}
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, redirect)
	if err != nil || !plan.Applicable() || plan.RequiresSecondApprover || len(plan.SecondApproverReasons) != 0 {
		t.Fatalf("a redirect in a process with no control: %+v %v, want it to need nobody else", plan, err)
	}
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, redirect, servicecontracts.WithActor("dita")); err != nil {
		t.Fatalf("the redirect on one call: %v", err)
	}
	f.assertWaitingAt(t, v2, "salesApprove")
	if n := f.requestCount(t); n != 0 {
		t.Fatalf("it left %d request(s)", n)
	}
}

// Nor does it ask once every instance has passed every control: there is
// nothing left for a redirect to take anybody past.
func TestARedirectAsksNobodyOnceEveryControlHasBeenPassed(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, prepared(f.project, true, true), prepared(f.project, true, true))
	f.completeTaskOn(t, "prepare", "pat")
	f.completeTaskOn(t, "control", "cyd")
	f.assertWaitingAt(t, v1, "sign")
	back := map[string]string{"prepare": "sign"}
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, back)
	if err != nil || !plan.Applicable() || plan.RequiresSecondApprover {
		t.Fatalf("a redirect once the control has been passed: requires=%v reasons=%v (err %v)", plan.RequiresSecondApprover, plan.SecondApproverReasons, err)
	}
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, back, servicecontracts.WithActor("dita")); err != nil {
		t.Fatalf("the redirect on one call: %v", err)
	}
	f.assertWaitingAt(t, v2, "sign")
}

// awaited is start → wait for the ERP → the control → wait for the bank → end:
// two waits nobody holds a task at, with a control between them.
func awaited(projectID uuid.UUID) *entities.ProcessDefinition {
	wait := func(id, name, message, in, out string) *entities.Node {
		return &entities.Node{ID: id, Name: name, Type: entities.IntermediateCatchEvent,
			Properties: map[string]any{"message_name": message}, Incoming: []string{in}, Outgoing: []string{out}}
	}
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "awaited-control",
		Name:    "Awaited control",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"a1"}},
			wait("erp", "Wait for the ERP", "erp-replied", "a1", "a2"),
			{ID: "control", Name: "Second signature", Type: entities.UserTask, Assignee: "cyd",
				Properties: map[string]any{"compliance_relevant": true}, Incoming: []string{"a2"}, Outgoing: []string{"a3"}},
			wait("bank", "Wait for the bank", "bank-replied", "a3", "a4"),
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"a4"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "a1", SourceRef: "start", TargetRef: "erp"},
			{ID: "a2", SourceRef: "erp", TargetRef: "control"},
			{ID: "a3", SourceRef: "control", TargetRef: "bank"},
			{ID: "a4", SourceRef: "bank", TargetRef: "end"},
		},
	}
}

// An approved redirect says which step each instance was moved from and to,
// whether or not anybody held a task there. A wait for a message has no
// task: its redirect used to be listed nowhere, and the entry of a migration
// that took an instance past a control said only that it changed version.
func TestAnApprovedRedirectOfAStepNobodyHoldsSaysWhereItMovedFromAndTo(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, awaited(f.project), awaited(f.project))
	instance := f.onlyInstance(t)
	if len(instance.Tokens) != 1 || instance.Tokens[0].Node == nil || instance.Tokens[0].Node.ID != "erp" || len(f.openTasks(t)) != 0 {
		t.Fatalf("the instance holds %v with %d open task(s), want it waiting for the ERP with nobody holding a task", tokenNodes(instance), len(f.openTasks(t)))
	}
	past := map[string]string{"erp": "bank"}
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, past, servicecontracts.WithActor("dita"))
	if err != nil || !plan.Applicable() || !plan.RequiresSecondApprover {
		t.Fatalf("the plan: applicable=%v refusals=%v requires=%v (err %v)", plan.Applicable(), plan.Refusals, plan.RequiresSecondApprover, err)
	}
	result, err := f.applyWithApproval(t, v1, v2, past, servicecontracts.WithActor("dita"))
	if err != nil || result.Changed != 1 {
		t.Fatalf("the approved redirect: %+v %v", result, err)
	}
	moved := f.onlyInstance(t)
	if moved.Definition == nil || moved.Definition.ID != v2 || len(moved.Tokens) != 1 || moved.Tokens[0].Node.ID != "bank" {
		t.Fatalf("the instance holds %v on %v, want it waiting for the bank on the new version", tokenNodes(moved), moved.Definition)
	}
	entry := f.entryOf(t, instance.ID, impl.EventInstanceMigrated)
	if moves, _ := entry.Data["task_moves"].([]any); len(moves) != 1 || moves[0] != "erp→bank" || !strings.Contains(entry.Narrative, "erp→bank") ||
		entry.Data["approved_by"] != "omar" {
		t.Fatalf("the entry of the approved redirect: %q %v, want the step it was moved from and to", entry.Narrative, entry.Data)
	}
}
