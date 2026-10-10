package instancemigration

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// A rename and a redirect are told apart by more than whether the id a step
// is mapped to is new. A step is renamed when its old id is gone from the new
// version and the step under the new id stands where it stood: the same steps
// lead to it and it leads to the same steps. Anything else sends its work
// somewhere else — and is asked about as that.

// step is a user task of a line: its id, its name, and whether it is marked
// as a control.
type step struct {
	id, name string
	control  bool
}

// lined is start → each step in turn → end, under one key.
func lined(projectID uuid.UUID, steps ...step) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "lined",
		Name:    "Lined",
		Nodes:   []*entities.Node{{ID: "start", Type: entities.StartEvent}},
	}
	for _, s := range steps {
		node := &entities.Node{ID: s.id, Name: s.name, Type: entities.UserTask, Assignee: "ada"}
		if s.control {
			node.Properties = map[string]any{"compliance_relevant": true}
		}
		def.Nodes = append(def.Nodes, node)
	}
	def.Nodes = append(def.Nodes, &entities.Node{ID: "end", Type: entities.EndEvent})
	for n := 1; n < len(def.Nodes); n++ {
		from, to := def.Nodes[n-1], def.Nodes[n]
		flow := fmt.Sprintf("l%d", n)
		from.Outgoing, to.Incoming = []string{flow}, []string{flow}
		def.Flows = append(def.Flows, &entities.SequenceFlow{ID: flow, SourceRef: from.ID, TargetRef: to.ID})
	}
	return def
}

var (
	prepareStep = step{id: "prepare", name: "Prepare"}
	controlStep = step{id: "control", name: "Second signature", control: true}
	signStep    = step{id: "sign", name: "Sign"}
	fileStep    = step{id: "file", name: "File"}
)

// requireAsks fails unless the plan needs a second administrator, the apply
// on one administrator's call is refused, and nothing moved.
func (f *fixture) requireAsks(t *testing.T, v1, v2 uuid.UUID, mapping map[string]string, at string, opts ...servicecontracts.MigrationOption) entities.MigrationPlan {
	t.Helper()
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, mapping, opts...)
	if err != nil || !plan.Applicable() {
		t.Fatalf("the plan: applicable=%v refusals=%v (err %v)", plan.Applicable(), plan.Refusals, err)
	}
	if !plan.RequiresSecondApprover {
		t.Fatalf("the mapping %v needs nobody else: reasons=%v holds=%+v", mapping, plan.SecondApproverReasons, plan.ComplianceHolds)
	}
	if _, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, mapping, opts...); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("the mapping %v on one administrator's call: %v, want it forbidden", mapping, err)
	}
	f.assertWaitingAt(t, v1, at)
	return plan
}

// TestAMappingOntoAStepNewInTheTargetIsARedirectWhileTheOldStepIsStillThere.
//
// Root cause: a mapping counted as a rename whenever the id it mapped to was
// not a step of the source version. The new version keeps "prepare" and adds
// "file" after the signature; "prepare → file" is then no rename — prepare is
// still there — and it takes the instance past the control and the signature.
// Counted as a rename, it held nothing, asked nobody, and was not even warned
// of.
func TestAMappingOntoAStepNewInTheTargetIsARedirectWhileTheOldStepIsStillThere(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, lined(f.project, prepareStep, controlStep, signStep), lined(f.project, prepareStep, controlStep, signStep, fileStep))
	f.assertWaitingAt(t, v1, "prepare")
	plan := f.requireAsks(t, v1, v2, map[string]string{"prepare": "file"}, "prepare", servicecontracts.WithActor("dita"))
	if len(plan.SecondApproverReasons) != 1 || !strings.Contains(plan.SecondApproverReasons[0], "“Prepare” would be redirected to “File”") ||
		!strings.Contains(plan.SecondApproverReasons[0], "“Second signature”") {
		t.Fatalf("the reason: %v", plan.SecondApproverReasons)
	}
}

// The twin, for a control: two controls in a row, a third added after them,
// and the first mapped onto the third. It is not the first control renamed —
// the first is still there — and neither it nor the second is performed.
func TestAControlMappedOntoAControlNewInTheTargetIsAControlNotCarriedAcross(t *testing.T) {
	f := newFixture(t)
	c1, c2, c3 := step{"c1", "First check", true}, step{"c2", "Second check", true}, step{"c3", "Third check", true}
	v1, v2 := f.startedOn(t, lined(f.project, c1, c2), lined(f.project, c1, c2, c3))
	f.assertWaitingAt(t, v1, "c1")
	onto := map[string]string{"c1": "c3"}
	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, onto, servicecontracts.WithActor("dita"))
	if err != nil {
		t.Fatalf("the plan: %v", err)
	}
	if len(plan.ComplianceHolds) != 1 || plan.ComplianceHolds[0].NodeID != "c1" || plan.Applicable() {
		t.Fatalf("the plan holds %+v and refuses %v, want the first check held until somebody accepts its loss", plan.ComplianceHolds, plan.Refusals)
	}
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, onto, servicecontracts.WithActor("dita")); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("the mapping with nothing acknowledged: %v, want it refused", err)
	}
	f.assertWaitingAt(t, v1, "c1")
	f.requireAsks(t, v1, v2, onto, "c1", servicecontracts.WithAcknowledgedHolds("c1"), servicecontracts.WithActor("dita"))
}

// The residual: the new version drops "prepare" and adds "file" after the
// control. By ids alone "prepare → file" is a rename — the old id is gone and
// the new one is new. It is not: "file" does not stand where "prepare" stood.
func TestAMappingOntoANewStepThatStandsElsewhereIsARedirectThoughTheOldStepIsGone(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, lined(f.project, prepareStep, controlStep, signStep), lined(f.project, controlStep, signStep, fileStep))
	f.assertWaitingAt(t, v1, "prepare")
	plan := f.requireAsks(t, v1, v2, map[string]string{"prepare": "file"}, "prepare", servicecontracts.WithActor("dita"))
	if !slices.ContainsFunc(plan.SecondApproverReasons, func(reason string) bool {
		return strings.Contains(reason, "“Prepare” would be redirected to “File”")
	}) {
		t.Fatalf("the reasons: %v", plan.SecondApproverReasons)
	}
}

// A step renamed in place — a new id, the same neighbours — is the same step.
// An ordinary one asks nobody, though the instance has a control still to
// pass; a control renamed in place is still that control, and nothing is held.
func TestAStepRenamedInPlaceAsksNobody(t *testing.T) {
	actor := servicecontracts.WithActor("dita")
	t.Run("an ordinary step, with a control still to pass", func(t *testing.T) {
		f := newFixture(t)
		v1, v2 := f.startedOn(t, lined(f.project, prepareStep, controlStep, signStep), lined(f.project, step{id: "draft", name: "Draft"}, controlStep, signStep))
		f.assertWaitingAt(t, v1, "prepare")
		rename := map[string]string{"prepare": "draft"}
		plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, rename, actor)
		if err != nil || !plan.Applicable() || plan.RequiresSecondApprover || len(plan.ComplianceHolds) != 0 {
			t.Fatalf("a rename in place: applicable=%v requires=%v reasons=%v holds=%+v (err %v)", plan.Applicable(), plan.RequiresSecondApprover, plan.SecondApproverReasons, plan.ComplianceHolds, err)
		}
		if err := f.svc.MigrateInstances(f.ctx, v1, v2, rename, actor); err != nil {
			t.Fatalf("the rename on one administrator's call: %v", err)
		}
		f.assertWaitingAt(t, v2, "draft")
		if n := f.requestCount(t); n != 0 {
			t.Fatalf("it left %d request(s)", n)
		}
	})
	t.Run("a control", func(t *testing.T) {
		f := newFixture(t)
		v1, v2 := f.startedOn(t, lined(f.project, prepareStep, controlStep, signStep),
			lined(f.project, prepareStep, step{id: "countersign", name: "Countersign", control: true}, signStep))
		f.completeTaskOn(t, "prepare", "ada")
		f.assertWaitingAt(t, v1, "control")
		rename := map[string]string{"control": "countersign"}
		plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, rename, actor)
		if err != nil || !plan.Applicable() || plan.RequiresSecondApprover || len(plan.ComplianceHolds) != 0 {
			t.Fatalf("a control renamed in place: applicable=%v refusals=%v requires=%v reasons=%v holds=%+v (err %v)",
				plan.Applicable(), plan.Refusals, plan.RequiresSecondApprover, plan.SecondApproverReasons, plan.ComplianceHolds, err)
		}
		if err := f.svc.MigrateInstances(f.ctx, v1, v2, rename, actor); err != nil {
			t.Fatalf("the rename on one administrator's call: %v", err)
		}
		instance := f.assertWaitingAt(t, v2, "countersign")
		if rows := f.ledger(t, instance.ID); len(rows) != 0 {
			t.Fatalf("a control renamed in place left %d ledger row(s)", len(rows))
		}
	})
}

// TestFinishedWorkDoesNotFollowAMappingOfAStepTheNewVersionStillHas.
//
// Root cause: the same notion of a rename decided which finished work takes
// its step's new id. The instance has finished "prepare" and waits at the
// control; the new version keeps "prepare" and adds a third, marked step
// after the signature. "prepare → audit" recorded the finished preparation as
// the audit — a control the instance has never been near — performed.
func TestFinishedWorkDoesNotFollowAMappingOfAStepTheNewVersionStillHas(t *testing.T) {
	f := newFixture(t)
	audit := step{id: "audit", name: "Audit", control: true}
	v1, v2 := f.startedOn(t, lined(f.project, prepareStep, controlStep, signStep), lined(f.project, prepareStep, controlStep, signStep, audit))
	f.completeTaskOn(t, "prepare", "ada")
	instance := f.assertWaitingAt(t, v1, "control")
	before := f.finishedTasks(t, instance.ID)

	mapping := map[string]string{"prepare": "audit"}
	if _, err := f.applyWithApproval(t, v1, v2, mapping, servicecontracts.WithActor("dita")); err != nil {
		t.Fatalf("apply: %v", err)
	}
	moved := f.assertWaitingAt(t, v2, "control")
	if done := nodeIDs(moved.CompletedNodes); slices.ContainsFunc(done, func(id string) bool { return strings.HasPrefix(id, "audit") }) {
		t.Errorf("the instance's completed steps are %v; it has not performed the audit", done)
	}
	assertRowsUntouched(t, "finished task", before, f.finishedTasks(t, instance.ID))
}
