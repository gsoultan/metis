package instancemigration

import (
	"maps"
	"slices"
	"strings"
	"testing"

	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// A finished ordinary step must never become evidence that a control was
// performed. Finished work follows a rename; onto a step marked as a control
// it follows only a rename in place — the control itself, under a new id.

// performed reports whether an instance's completed steps include the step.
func performed(done []string, step string) bool {
	return slices.ContainsFunc(done, func(id string) bool { return strings.HasPrefix(id, step) })
}

// TestFinishedWorkDoesNotFollowARenameOntoAControlThatStandsElsewhere.
//
// Root cause: which finished work takes a step's new id was decided from ids
// alone. The new version drops "prepare" and adds a marked "audit" after the
// signature; "prepare → audit" is then a rename by ids — the old id is gone,
// the new one is new — and the instance's finished preparation was recorded
// as the audit: in its completed steps and on the finished task. The audit
// read as passed, was never performed, and a later migration that dropped it
// would have held nothing for this instance.
func TestFinishedWorkDoesNotFollowARenameOntoAControlThatStandsElsewhere(t *testing.T) {
	f := newFixture(t)
	audit := step{id: "audit", name: "Audit", control: true}
	v1, v2 := f.startedOn(t, lined(f.project, prepareStep, signStep), lined(f.project, signStep, audit))
	f.completeTaskOn(t, "prepare", "ada")
	instance := f.assertWaitingAt(t, v1, "sign")
	before := f.finishedTasks(t, instance.ID)
	mapping := map[string]string{"prepare": "audit"}
	actor := servicecontracts.WithActor("dita")

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, mapping, actor)
	if err != nil || !plan.Applicable() {
		t.Fatalf("the plan: applicable=%v refusals=%v (err %v)", plan.Applicable(), plan.Refusals, err)
	}
	// The plan says, before anybody applies it, that the preparation done
	// will not count as the audit.
	if !slices.ContainsFunc(plan.Warnings, func(warning string) bool {
		return strings.Contains(warning, `"prepare" is mapped onto "audit", which is a different step`) &&
			strings.Contains(warning, `1 running instance(s) have completed "prepare"`)
	}) {
		t.Errorf("the plan does not warn that work done on the preparation is not the audit done: %v", plan.Warnings)
	}
	if _, err := f.applyOnOneCall(t, v1, v2, mapping, actor); err != nil {
		t.Fatalf("apply: %v", err)
	}
	moved := f.assertWaitingAt(t, v2, "sign")
	done := nodeIDs(moved.CompletedNodes)
	if performed(done, "audit") || !performed(done, "prepare") {
		t.Errorf("the instance's completed steps are %v; it prepared, and has not performed the audit", done)
	}
	assertRowsUntouched(t, "finished task", before, f.finishedTasks(t, instance.ID))
}

// A control renamed in place is still that control, and whoever passed it
// has passed it: its finished work goes with it to the new id, as an
// ordinary step's does.
func TestAControlRenamedInPlaceCarriesItsFinishedWork(t *testing.T) {
	f := newFixture(t)
	countersign := step{id: "countersign", name: "Countersign", control: true}
	v1, v2 := f.startedOn(t, lined(f.project, prepareStep, controlStep, signStep), lined(f.project, prepareStep, countersign, signStep))
	f.completeTaskOn(t, "prepare", "ada")
	f.completeTaskOn(t, "control", "ada")
	instance := f.assertWaitingAt(t, v1, "sign")
	before := f.finishedTasks(t, instance.ID)
	rename := map[string]string{"control": "countersign"}
	actor := servicecontracts.WithActor("dita")

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, rename, actor)
	if err != nil || !plan.Applicable() || plan.RequiresSecondApprover || len(plan.ComplianceHolds) != 0 || len(plan.Warnings) != 0 {
		t.Fatalf("a control renamed in place: applicable=%v requires=%v holds=%+v warnings=%v (err %v)",
			plan.Applicable(), plan.RequiresSecondApprover, plan.ComplianceHolds, plan.Warnings, err)
	}
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, rename, actor); err != nil {
		t.Fatalf("the rename on one administrator's call: %v", err)
	}
	moved := f.assertWaitingAt(t, v2, "sign")
	done := nodeIDs(moved.CompletedNodes)
	if !performed(done, "countersign") || slices.ContainsFunc(done, func(id string) bool { return id == "control" }) {
		t.Errorf("the instance's completed steps are %v; it passed the control, which is now the countersignature", done)
	}
	// The finished control is the same row, on the step's new id and in
	// nothing else different.
	renamed := maps.Clone(before)
	for id, told := range renamed {
		renamed[id] = strings.Replace(told, "node=control ", "node=countersign ", 1)
	}
	assertRowsUntouched(t, "finished task, but for the control's new id", renamed, f.finishedTasks(t, instance.ID))
}
