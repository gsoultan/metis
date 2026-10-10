package instancemigration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/endpoints/definition"
)

// What an apply says about an instance it left alone.
//
// A migration that finds, once it holds an instance's lock, that the instance
// is no longer where the listing found it does nothing to that instance. The
// reply said "applied: true" and nothing else, so the person who pressed the
// button was told the migration was done while an instance was still running
// the old version. The reply now lists each one with the reason, and "applied"
// is false when nothing at all was written.

// migrationReply is the endpoint's answer as a client reads it: off the wire.
type migrationReply struct {
	Applied    *bool `json:"applied"`
	PassedOver *[]struct {
		InstanceID string `json:"instance_id"`
		Reason     string `json:"reason"`
	} `json:"passed_over"`
}

// migrateOverTheEndpoint asks the migration endpoint and reads its reply as
// JSON, so the names and the shape a client sees are what is asserted.
func (f *fixture) migrateOverTheEndpoint(t *testing.T, request definition.MigrateInstancesRequest) (migrationReply, string) {
	t.Helper()
	reply := f.answerOf(t, request)
	body, err := json.Marshal(reply)
	if err != nil {
		t.Fatalf("encode the reply: %v", err)
	}
	var read migrationReply
	if err := json.Unmarshal(body, &read); err != nil {
		t.Fatalf("decode the reply: %v (%s)", err, body)
	}
	if read.Applied == nil || read.PassedOver == nil {
		t.Errorf("the reply must always say applied and passed_over, an empty list when none: %s", body)
		// So that what the caller goes on to assert is still judged.
		var no bool
		var nobody []struct {
			InstanceID string `json:"instance_id"`
			Reason     string `json:"reason"`
		}
		if read.Applied == nil {
			read.Applied = &no
		}
		if read.PassedOver == nil {
			read.PassedOver = &nobody
		}
	}
	return read, string(body)
}

// atTheEndpointsApplyListing runs fire when an apply asked through the
// endpoint has listed its instances. The endpoint plans, to answer with the
// plan; MigrateInstances plans again, so that a preview and an apply cannot
// disagree; the apply's own listing is the third.
func (p *hookedProcess) atTheEndpointsApplyListing(fire func()) {
	p.on = p.calls + 3
	p.fire = fire
}

// skipOpsRequest is an apply, or a dry run, of v1 → v2 that skips the
// operations approval.
func skipOpsRequest(v1, v2 string, dryRun bool) definition.MigrateInstancesRequest {
	return definition.MigrateInstancesRequest{
		SourceDefinitionID: v1, TargetDefinitionID: v2, DryRun: &dryRun,
		NodeActions: map[string]servicecontracts.NodeAction{
			"opsApprove": {Kind: servicecontracts.NodeActionSkip, Reason: "the role was eliminated"},
		},
	}
}

// stillOn is the instances of the project still running the version given.
func (f *fixture) stillOn(t *testing.T, version string) []entities.ProcessInstance {
	t.Helper()
	all, err := f.svc.ListInstances(f.ctx, f.project)
	if err != nil {
		t.Fatalf("list instances: %v", err)
	}
	var on []entities.ProcessInstance
	for _, instance := range all {
		if instance.Definition != nil && instance.Definition.ID.String() == version {
			on = append(on, instance)
		}
	}
	return on
}

// TestAnApplyNamesTheInstanceItPassedOver.
//
// Root cause: apply knew which instances it had left alone and told only the
// server log; MigrateInstances returns an error or nothing, so the reply could
// not say. Two instances wait at the operations approval; the holder of one
// completes it after the apply listed them. The other is skipped and moved,
// so something was written: applied is true, and the one left alone is named,
// with a reason that names the step as people know it.
func TestAnApplyNamesTheInstanceItPassedOver(t *testing.T) {
	f, listing := newRacedFixture(t)
	// Both started before the second version exists: a new instance starts on
	// the latest one.
	first, err := f.svc.CreateDefinition(f.ctx, quotationV1(f))
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	for range 2 {
		if _, err := f.svc.StartProcess(f.ctx, f.project, "quotation", nil); err != nil {
			t.Fatalf("start a quotation: %v", err)
		}
		f.completeTaskOn(t, "supervisorReview", "sam")
	}
	second, err := f.svc.CreateDefinition(f.ctx, quotationV2(f))
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	v1, v2 := first.String(), second.String()
	listing.atTheEndpointsApplyListing(func() { f.completeTaskOn(t, "opsApprove", "ollie") })

	reply, body := f.migrateOverTheEndpoint(t, skipOpsRequest(v1, v2, false))

	left := f.stillOn(t, v1)
	if len(left) != 1 || len(f.stillOn(t, v2)) != 1 {
		t.Fatalf("%d instance(s) are on v1 and %d on v2; want the one passed over and the one skipped and moved",
			len(left), len(f.stillOn(t, v2)))
	}
	if !*reply.Applied {
		t.Errorf("applied is false although one instance was skipped and moved: %s", body)
	}
	if len(*reply.PassedOver) != 1 || (*reply.PassedOver)[0].InstanceID != left[0].ID.String() {
		t.Fatalf("passed_over is %+v, want the one instance still on v1 (%s)", *reply.PassedOver, left[0].ID)
	}
	reason := (*reply.PassedOver)[0].Reason
	if !strings.Contains(reason, "Operations approve") || strings.Contains(reason, "opsApprove") {
		t.Errorf("the reason should name the step as people know it and not by its id: %q", reason)
	}
	if rows := f.ledger(t, left[0].ID); len(rows) != 0 {
		t.Errorf("the instance passed over has %d ledger row(s)", len(rows))
	}
}

// The only instance moved on, so the run wrote nothing: "applied" would be a
// false statement, and the reply says which instance and why.
func TestAnApplyThatPassedOverEveryInstanceSaysNothingWasApplied(t *testing.T) {
	f, listing := newRacedFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)
	listing.atTheEndpointsApplyListing(func() { f.completeTaskOn(t, "opsApprove", "ollie") })

	reply, body := f.migrateOverTheEndpoint(t, skipOpsRequest(v1, v2, false))

	instance := f.onlyInstance(t)
	if *reply.Applied {
		t.Errorf("applied is true although nothing was written to any instance: %s", body)
	}
	if len(*reply.PassedOver) != 1 || (*reply.PassedOver)[0].InstanceID != instance.ID.String() {
		t.Fatalf("passed_over is %+v, want the one instance (%s)", *reply.PassedOver, instance.ID)
	}
	f.assertOnlyItsHolderActed(t, v1)
}

// An instance that finished before the run reached it is left alone too, in a
// migration that only moves work as in one that decides it.
func TestAnApplyNamesAnInstanceThatFinishedBeforeItWasReached(t *testing.T) {
	f, listing := newRacedFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)
	listing.atTheEndpointsApplyListing(func() {
		f.completeTaskOn(t, "opsApprove", "ollie")
		f.completeTaskOn(t, "salesApprove", "sasha")
	})
	dryRun := false
	reply, body := f.migrateOverTheEndpoint(t, definition.MigrateInstancesRequest{
		SourceDefinitionID: v1, TargetDefinitionID: v2, DryRun: &dryRun,
		NodeMapping: map[string]string{"opsApprove": "salesApprove"},
	})

	instance := f.onlyInstance(t)
	if instance.Status != entities.ProcessCompleted || instance.Definition == nil || instance.Definition.ID.String() != v1 {
		t.Fatalf("the instance is %s on %v; it finished on v1 before the run reached it", instance.Status, instance.Definition)
	}
	if *reply.Applied {
		t.Errorf("applied is true although nothing was written to any instance: %s", body)
	}
	if len(*reply.PassedOver) != 1 || (*reply.PassedOver)[0].InstanceID != instance.ID.String() {
		t.Fatalf("passed_over is %+v, want the one instance (%s)", *reply.PassedOver, instance.ID)
	}
	if reason := (*reply.PassedOver)[0].Reason; !strings.Contains(reason, "no longer running") {
		t.Errorf("the reason should say the instance had finished: %q", reason)
	}
}

// An ordinary run leaves nobody behind and says so with an empty list, not a
// missing field; a dry run, which writes nothing, has passed nothing over.
func TestAnOrdinaryApplyAndADryRunPassNothingOver(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.parkedOnOpsApprove(t)

	preview, body := f.migrateOverTheEndpoint(t, skipOpsRequest(v1, v2, true))
	if *preview.Applied || len(*preview.PassedOver) != 0 || !strings.Contains(body, `"passed_over":[]`) {
		t.Errorf("a dry run: %s, want applied false and an empty passed_over", body)
	}

	reply, body := f.migrateOverTheEndpoint(t, skipOpsRequest(v1, v2, false))
	if !*reply.Applied || len(*reply.PassedOver) != 0 || !strings.Contains(body, `"passed_over":[]`) {
		t.Errorf("an ordinary apply: %s, want applied true and an empty passed_over", body)
	}
	if len(f.stillOn(t, v2)) != 1 {
		t.Errorf("the instance was not moved to the new version")
	}
}
