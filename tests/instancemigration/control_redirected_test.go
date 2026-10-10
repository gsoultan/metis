package instancemigration

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// A control mapped onto a different step is a control nobody performs,
// whatever the step it lands on is marked as.

// twoControls is start → first check → second check → end, both marked as
// controls: two steps each of which has to be performed.
func twoControls(projectID uuid.UUID) *entities.ProcessDefinition {
	marked := func(note string) map[string]any {
		return map[string]any{"compliance_relevant": true, "compliance_note": note}
	}
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "two-controls",
		Name:    "Two controls",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"t1"}},
			{ID: "c1", Name: "First check", Type: entities.UserTask, Assignee: "cyd", Properties: marked("maker"), Incoming: []string{"t1"}, Outgoing: []string{"t2"}},
			{ID: "c2", Name: "Second check", Type: entities.UserTask, Assignee: "dee", Properties: marked("checker"), Incoming: []string{"t2"}, Outgoing: []string{"t3"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"t3"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "t1", SourceRef: "start", TargetRef: "c1"},
			{ID: "t2", SourceRef: "c1", TargetRef: "c2"},
			{ID: "t3", SourceRef: "c2", TargetRef: "end"},
		},
	}
}

// R2. Two controls in a row, the instance waiting at the first, and a mapping
// that sends the first onto the second. The first is never performed.
//
// Root cause: a control counted as carried across whenever the step its
// mapping landed on was marked too. Performing the second check is not
// performing the first: the plan held nothing, and one administrator applied
// it.
func TestAControlRedirectedOntoAnotherControlIsAControlNotCarriedAcross(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, twoControls(f.project), twoControls(f.project))
	instance := f.assertWaitingAt(t, v1, "c1")
	onto := map[string]string{"c1": "c2"}
	actor := servicecontracts.WithActor("dita")

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, onto, actor)
	if err != nil {
		t.Fatalf("the plan: %v", err)
	}
	if len(plan.ComplianceHolds) != 1 || plan.ComplianceHolds[0].NodeID != "c1" || plan.ComplianceHolds[0].Instances != 1 {
		t.Fatalf("the plan holds %+v, want the first check, for the one instance that has not passed it", plan.ComplianceHolds)
	}
	if plan.Applicable() || !slices.ContainsFunc(plan.Refusals, func(refusal string) bool {
		return strings.Contains(refusal, `"c1" is marked as carrying a control obligation`) && strings.Contains(refusal, "acknowledge it explicitly")
	}) {
		t.Fatalf("the plan is not refused until somebody accepts the loss: %v", plan.Refusals)
	}
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, onto, actor); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("the redirect with nothing acknowledged: %v, want it refused", err)
	}
	f.assertWaitingAt(t, v1, "c1")

	// Acknowledged, it is a control dropped for an instance that has not
	// passed it: a second administrator's to approve.
	accepted := []servicecontracts.MigrationOption{servicecontracts.WithAcknowledgedHolds("c1"), actor}
	plan, err = f.svc.PlanInstanceMigration(f.ctx, v1, v2, onto, accepted...)
	if err != nil || !plan.Applicable() || !plan.RequiresSecondApprover ||
		!slices.Contains(plan.SecondApproverReasons, "“First check” carries a control 1 instance(s) have not passed, and they never would") {
		t.Fatalf("the acknowledged plan: applicable=%v requires=%v reasons=%v (err %v)", plan.Applicable(), plan.RequiresSecondApprover, plan.SecondApproverReasons, err)
	}
	if _, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, onto, accepted...); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("the acknowledged redirect on one administrator's call: %v, want it forbidden", err)
	}
	f.assertWaitingAt(t, v1, "c1")
	if rows := f.ledger(t, instance.ID); len(rows) != 0 {
		t.Fatalf("the refused redirect left %d ledger row(s)", len(rows))
	}

	// Approved, the instance moves, and the loss of the first check is in the
	// ledger under the request.
	result, err := f.applyWithApproval(t, v1, v2, onto, accepted...)
	if err != nil || result.Changed != 1 {
		t.Fatalf("the approved redirect: %+v %v", result, err)
	}
	f.assertWaitingAt(t, v2, "c2")
	rows := f.ledger(t, instance.ID)
	if len(rows) != 1 || rows[0].Kind != entities.DeviationControlWaived || rows[0].Node == nil || rows[0].Node.ID != "c1" ||
		rows[0].RequestID != f.underApproval || rows[0].ApprovedBy != "omar" || rows[0].Actor != "dita" {
		t.Fatalf("the ledger: %+v, want the one control_waived row for the first check, under the request", rows)
	}
}
