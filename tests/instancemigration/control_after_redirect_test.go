package instancemigration

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// A redirect, and then the control it redirected onto is dropped.
//
// An instance keeps a list of the steps it has completed, and a migration that
// drops a control step asks that list which instances have not passed the
// control yet: those are the ones whose loss somebody has to accept. The list
// used to follow every mapping. A finished step redirected onto a control the
// instance was only waiting at put that control in the list, as though it had
// been performed.

// prepared is prepare → the control → sign, with each present or not: the
// three versions of the process this test walks through.
func prepared(projectID uuid.UUID, withPrepare, withControl bool) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "prepared-control",
		Name:    "Prepared control",
		Nodes:   []*entities.Node{{ID: "start", Type: entities.StartEvent, Outgoing: []string{"s1"}}},
	}
	steps := []*entities.Node{}
	if withPrepare {
		steps = append(steps, &entities.Node{ID: "prepare", Name: "Prepare", Type: entities.UserTask, Assignee: "pat"})
	}
	if withControl {
		steps = append(steps, &entities.Node{
			ID: "control", Name: "Second signature", Type: entities.UserTask, Assignee: "cyd",
			Properties: map[string]any{"compliance_relevant": true, "compliance_note": "second signature required over $50k"},
		})
	}
	steps = append(steps,
		&entities.Node{ID: "sign", Name: "Sign", Type: entities.UserTask, Assignee: "sasha"},
		&entities.Node{ID: "end", Type: entities.EndEvent})
	previous := def.Nodes[0]
	for n, step := range steps {
		flow := "s" + string(rune('1'+n))
		previous.Outgoing = []string{flow}
		step.Incoming = []string{flow}
		def.Flows = append(def.Flows, &entities.SequenceFlow{ID: flow, SourceRef: previous.ID, TargetRef: step.ID})
		def.Nodes = append(def.Nodes, step)
		previous = step
	}
	return def
}

// TestAControlAnInstanceOnlyWaitedAtIsStillHeldAfterARedirectOntoIt.
//
// Root cause: the instance's completed-steps list was rewritten through every
// mapping, a redirect included, so a step it had finished was recorded as the
// step the mapping pointed at.
//
// The preparation is done and the instance waits at the control. Version 2
// drops the preparation, and the migration maps it onto the control. Version
// 3 then drops the control. Before: the instance's list read "control", the
// second migration's plan held nothing, it applied with nothing acknowledged,
// and no row said a control was lost. Without the first mapping the same
// migration is refused for the hold.
func TestAControlAnInstanceOnlyWaitedAtIsStillHeldAfterARedirectOntoIt(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, prepared(f.project, true, true), prepared(f.project, false, true))
	f.completeTaskOn(t, "prepare", "pat")
	instance := f.assertWaitingAt(t, v1, "control")

	// The first migration: the preparation is redirected onto the control.
	redirect := map[string]string{"prepare": "control"}
	first, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, redirect)
	if err != nil {
		t.Fatalf("plan the first: %v", err)
	}
	warned := strings.Join(first.Warnings, "; ")
	t.Logf("warned: %s", warned)
	for _, want := range []string{`"prepare"`, `"control"`, "does not count as done", "1 instance"} {
		if !strings.Contains(warned, want) {
			t.Errorf("the plan does not warn that %q: %v", want, first.Warnings)
		}
	}
	if !first.Applicable() {
		t.Fatalf("a warning must not refuse: %v", first.Refusals)
	}
	if _, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, redirect, servicecontracts.WithActor("dita")); err != nil {
		t.Fatalf("apply the first: %v", err)
	}
	moved := f.assertWaitingAt(t, v2, "control")
	if done := nodeIDs(moved.CompletedNodes); slices.ContainsFunc(done, func(id string) bool { return strings.HasPrefix(id, "control") }) {
		t.Errorf("the instance's completed steps are %v; it has not performed the control, it is waiting at it", done)
	}

	// The second: version 3 drops the control. The instance has not passed
	// it, so the plan holds, and the apply is refused until somebody accepts.
	v3, err := f.svc.CreateDefinition(f.ctx, prepared(f.project, false, false))
	if err != nil {
		t.Fatalf("deploy v3: %v", err)
	}
	drop := map[string]string{"control": "sign"}
	second, err := f.svc.PlanInstanceMigration(f.ctx, v2, v3, drop)
	if err != nil {
		t.Fatalf("plan the second: %v", err)
	}
	if len(second.ComplianceHolds) != 1 || second.ComplianceHolds[0].NodeID != "control" || second.ComplianceHolds[0].Instances != 1 || second.Applicable() {
		t.Fatalf("the plan should hold on the control for the one instance that has not passed it: holds=%+v refusals=%v",
			second.ComplianceHolds, second.Refusals)
	}
	if err := f.svc.MigrateInstances(f.ctx, v2, v3, drop, servicecontracts.WithActor("dita")); err == nil {
		t.Fatal("the control was dropped with nobody having accepted its loss")
	}
	f.assertWaitingAt(t, v2, "control")
	if rows := f.ledger(t, instance.ID); len(rows) != 0 {
		t.Errorf("a refused migration left %d ledger row(s): %+v", len(rows), rows)
	}

	// Accepted by name, it goes through, and the loss is in the ledger.
	if err := f.svc.MigrateInstances(f.ctx, v2, v3, drop,
		servicecontracts.WithAcknowledgedHolds("control"), servicecontracts.WithActor("dita")); err != nil {
		t.Fatalf("an acknowledged hold was still refused: %v", err)
	}
	f.assertWaitingAt(t, v3, "sign")
	rows := f.ledger(t, instance.ID)
	if len(rows) != 1 || rows[0].Kind != entities.DeviationControlWaived || rows[0].Node == nil || rows[0].Node.ID != "control" {
		t.Errorf("the ledger should hold the one control_waived row for the control: %+v", rows)
	}
	f.assertNothingIsStranded(t)
	f.runsToItsEnd(t)
}

// A rename is not a redirect: the list follows it, and nothing is warned.
func TestTheCompletedStepsFollowARenameAndNothingIsWarned(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, fourEyes(f.project, "submit", "approve"), fourEyes(f.project, "request", "signOff"))
	f.completeTaskOn(t, "submit", "ada")
	rename := map[string]string{"submit": "request", "approve": "signOff"}
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, rename)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Warnings) != 0 {
		t.Errorf("a mapping that only renames steps was warned about: %v", plan.Warnings)
	}
	if _, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, rename, servicecontracts.WithActor("dita")); err != nil {
		t.Fatalf("apply: %v", err)
	}
	moved := f.assertWaitingAt(t, v2, "signOff")
	if done := nodeIDs(moved.CompletedNodes); len(done) != 2 || !strings.HasPrefix(done[1], "request") {
		t.Errorf("the instance's completed steps are %v, want the submit under its new id", done)
	}
	f.assertNothingIsStranded(t)
}
