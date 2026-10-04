package instancemigration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/endpoints/definition"
)

// An instance the plan never saw.
//
// An apply plans first, so that it refuses what a dry run would refuse, and
// then lists the instances again to work through them. Everything the plan
// asks of an instance — can its work land, does it lose a control somebody has
// to accept — was asked of the first list. An instance that started on the
// source version between the two was in the second list only: nothing had
// been asked about it, and it was moved all the same.

// atTheEndpointsApplyPlan runs fire when an apply asked through the endpoint
// has made its own plan and not yet listed the instances to work through: the
// endpoint plans, to answer with the plan, and the apply's plan is the second
// listing.
func (p *hookedProcess) atTheEndpointsApplyPlan(fire func()) {
	p.on = p.calls + 2
	p.fire = fire
}

// planOf is the plan in the endpoint's reply, as a client reads it.
func planOf(t *testing.T, body string) entities.MigrationPlan {
	t.Helper()
	var reply struct {
		Plan entities.MigrationPlan `json:"plan"`
	}
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		t.Fatalf("decode the plan: %v (%s)", err, body)
	}
	return reply.Plan
}

// TestAnInstanceThatStartedAfterThePlanIsNotMoved.
//
// Root cause: the plan and the apply each listed the source version's
// instances, and the apply acted on its own list. What the plan had worked
// out — which controls are lost, and by how many instances — covered only the
// first.
//
// The operations approval carries a control obligation and the new version
// drops it. The one running instance has given that approval, so the plan
// holds nothing and asks for no acknowledgement. The new version is staged,
// not yet live, so a request arriving now still starts on the old one: one
// does, between the apply's plan and its listing. It has not given the
// approval. It was moved to the sales approval with nobody having accepted
// that, and with no row in its ledger.
func TestAnInstanceThatStartedAfterThePlanIsNotMoved(t *testing.T) {
	f, listing := newRacedFixture(t)
	v1, err := f.svc.CreateDefinition(f.ctx, controlledTwoStep(f.project))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	planned, err := f.svc.StartProcess(f.ctx, f.project, "controlled-two-step", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	f.completeTaskOn(t, "opsApprove", "ada")
	// Staged: version 1 stays the one a new instance starts on.
	v2, err := f.svc.DeployDefinition(f.ctx, controlledTwoStepWithout(f.project), false)
	if err != nil {
		t.Fatalf("stage v2: %v", err)
	}
	request := func(dryRun bool, acknowledge ...string) definition.MigrateInstancesRequest {
		return definition.MigrateInstancesRequest{
			SourceDefinitionID: v1.String(), TargetDefinitionID: v2.String(), DryRun: &dryRun,
			NodeMapping: map[string]string{"opsApprove": "salesApprove"}, Acknowledge: acknowledge,
		}
	}

	// The dry run: one instance, nothing held, nothing to acknowledge.
	_, body := f.migrateOverTheEndpoint(t, request(true))
	if plan := planOf(t, body); plan.Instances != 1 || len(plan.ComplianceHolds) != 0 || !plan.Applicable() {
		t.Fatalf("the dry run should cover the one instance and hold nothing: %s", body)
	}

	var late uuid.UUID
	listing.atTheEndpointsApplyPlan(func() {
		started, startErr := f.svc.StartProcess(f.ctx, f.project, "controlled-two-step", nil)
		if startErr != nil {
			t.Errorf("start the late instance: %v", startErr)
		}
		late = started
	})
	reply, body := f.migrateOverTheEndpoint(t, request(false))
	reached(t, listing)

	// The instance the plan covered is moved, as the dry run said.
	if moved, getErr := f.svc.GetInstance(f.ctx, planned); getErr != nil || moved.Definition == nil || moved.Definition.ID != v2 {
		t.Errorf("the instance the plan covered was not moved (err %v)", getErr)
	}
	// The one it never saw is exactly where it started: on the old version,
	// at the control step, with nothing written about it.
	unplanned, err := f.svc.GetInstance(f.ctx, late)
	if err != nil {
		t.Fatalf("read the late instance: %v", err)
	}
	if unplanned.Definition == nil || unplanned.Definition.ID != v1 {
		t.Fatalf("an instance the plan never saw was moved, past a control nobody accepted the loss of")
	}
	if on := tokenNodes(unplanned); len(on) != 1 || !strings.HasPrefix(on[0], "opsApprove") {
		t.Errorf("the late instance holds %v, want its one token on the operations approval", on)
	}
	if rows := f.ledger(t, late); len(rows) != 0 {
		t.Errorf("the late instance has %d ledger row(s): %+v", len(rows), rows)
	}
	f.assertNoMigrationEntries(t, late)
	f.assertNothingIsStranded(t)

	// And the reply says so, in words, with what to do.
	if !*reply.Applied {
		t.Errorf("applied is false although the planned instance was moved: %s", body)
	}
	if len(*reply.PassedOver) != 1 || (*reply.PassedOver)[0].InstanceID != late.String() {
		t.Fatalf("passed_over is %+v, want the one late instance (%s)", *reply.PassedOver, late)
	}
	reason := (*reply.PassedOver)[0].Reason
	t.Logf("passed over, and told: %s", reason)
	if !strings.Contains(reason, "planned") || !strings.Contains(reason, "lan the migration again") || anyID.MatchString(reason) {
		t.Errorf("the reason should say the migration was planned without it, and to plan again, and carry no id: %q", reason)
	}

	// Planning again includes it, and now there is something to accept.
	_, body = f.migrateOverTheEndpoint(t, request(true))
	again := planOf(t, body)
	if again.Instances != 1 || len(again.ComplianceHolds) != 1 || again.ComplianceHolds[0].NodeID != "opsApprove" || again.Applicable() {
		t.Fatalf("planned again, the migration should hold on the operations approval for the one instance: %s", body)
	}
	reply, body = f.migrateOverTheEndpoint(t, request(false, "opsApprove"))
	if !*reply.Applied || len(*reply.PassedOver) != 0 {
		t.Errorf("accepted by name, the migration should move it: %s", body)
	}
	rows := f.ledger(t, late)
	if len(rows) != 1 || rows[0].Kind != entities.DeviationControlWaived {
		t.Errorf("the control it lost should be in its ledger: %+v", rows)
	}
	f.assertNothingIsStranded(t)
}
