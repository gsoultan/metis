package deviation

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A migration's plan as a route writes it: the fields the migrate route
// always wrote, in the order it wrote them, each list a list when it holds
// something and left out when it does not, and nothing null. The migrate
// route's own test (endpoints/definition) holds it to the bytes the route
// wrote before there was a view; this one holds the view to its own shape,
// whatever the entity's tags come to say.
func TestAMigrationPlanIsWrittenThroughItsView(t *testing.T) {
	t.Parallel()
	target := uuid.MustParse("0198f3a0-0000-7000-8000-000000000002")
	plan := entities.MigrationPlan{
		SourceKey: "quotation", SourceVersion: 1, TargetVersion: 2, TargetID: target, Instances: 3,
		Moves:           []entities.NodeMove{{From: "legal", To: "review", Tokens: 1, Tasks: 1, TasksClaimed: 1, Events: 2, Mapped: true}},
		Refusals:        []string{"a refusal"},
		Warnings:        []string{"a warning"},
		ComplianceHolds: []entities.ComplianceHold{{NodeID: "opsApprove", Name: "Operations approve", Note: "SOX", Instances: 2}},
		Actions:         []entities.PlannedNodeAction{{NodeID: "opsApprove", Name: "Operations approve", Kind: "skip", Reason: "retired"}},
		RemovedNodes:    []string{"opsApprove"}, RequiresSecondApprover: true, SecondApproverReasons: []string{"a reason"},
	}
	written, err := json.Marshal(MigrationPlanViewOf(plan))
	if err != nil {
		t.Fatalf("write the plan: %v", err)
	}
	const want = `{"source_key":"quotation","source_version":1,"target_version":2,"target_id":"0198f3a0-0000-7000-8000-000000000002","instances":3,` +
		`"moves":[{"from":"legal","to":"review","tokens":1,"tasks":1,"jobs":0,"tasks_claimed":1,"events":2,"mapped":true}],` +
		`"refusals":["a refusal"],"warnings":["a warning"],` +
		`"compliance_holds":[{"node_id":"opsApprove","name":"Operations approve","note":"SOX","instances":2}],` +
		`"actions":[{"node_id":"opsApprove","name":"Operations approve","kind":"skip","reason":"retired"}],` +
		`"removed_nodes":["opsApprove"],"requires_second_approver":true,"second_approver_reasons":["a reason"]}`
	if string(written) != want {
		t.Errorf("the plan is written\n  %s\nwant\n  %s", written, want)
	}

	// A plan that holds nothing says so in the fields that are always there,
	// and has no list at all — never a null.
	nothing, err := json.Marshal(MigrationPlanViewOf(entities.MigrationPlan{}))
	if err != nil {
		t.Fatalf("write the empty plan: %v", err)
	}
	if string(nothing) != `{"source_key":"","source_version":0,"target_version":0,"target_id":"00000000-0000-0000-0000-000000000000",`+
		`"instances":0,"requires_second_approver":false}` {
		t.Errorf("a plan that holds nothing is written %s", nothing)
	}
	// And a list that is there and empty stays a list.
	emptied, err := json.Marshal(MigrationPlanViewOf(entities.MigrationPlan{
		Moves: []entities.NodeMove{}, ComplianceHolds: []entities.ComplianceHold{}, Actions: []entities.PlannedNodeAction{}, Refusals: []string{},
	}))
	if err != nil {
		t.Fatalf("write the emptied plan: %v", err)
	}
	for _, list := range []string{`"moves":[]`, `"compliance_holds":[]`, `"actions":[]`, `"refusals":[]`} {
		if !strings.Contains(string(emptied), list) {
			t.Errorf("an empty list is not written as one (%s): %s", list, emptied)
		}
	}
	for _, encoded := range [][]byte{written, nothing, emptied} {
		if strings.Contains(string(encoded), "null") {
			t.Errorf("the plan holds a null: %s", encoded)
		}
	}
}

// An instance a run left alone, as either route that lists them writes it:
// the instance, the cause as a code, the steps the cause is about — a list
// always, empty for a cause about no step, each step by id and by name — and
// the sentence. No list is null, and none of them for a run that left nobody.
func TestAPassedOverInstanceIsWrittenWithItsCauseAndItsSteps(t *testing.T) {
	t.Parallel()
	first, second := uuid.MustParse("0198f3a0-0000-7000-8000-0000000000a1"), uuid.MustParse("0198f3a0-0000-7000-8000-0000000000a2")
	written, err := json.Marshal(PassedOverViewsOf([]entities.PassedOverInstance{
		{Instance: &entities.ProcessInstance{ID: first}, Cause: entities.PassedOverNowhereToLand,
			Steps:  []entities.PassedOverStep{{NodeID: "legal", Name: "Legal review"}, {NodeID: "gate", Name: "gate"}},
			Reason: "It had work there."},
		{Instance: &entities.ProcessInstance{ID: second}, Cause: entities.PassedOverNoLongerRunning, Reason: "It had finished."},
	}))
	if err != nil {
		t.Fatalf("write the list: %v", err)
	}
	const want = `[{"instance_id":"0198f3a0-0000-7000-8000-0000000000a1","cause":"nowhere_to_land",` +
		`"steps":[{"node_id":"legal","name":"Legal review"},{"node_id":"gate","name":"gate"}],"reason":"It had work there."},` +
		`{"instance_id":"0198f3a0-0000-7000-8000-0000000000a2","cause":"no_longer_running","steps":[],"reason":"It had finished."}]`
	if string(written) != want {
		t.Errorf("the list is written\n  %s\nwant\n  %s", written, want)
	}
	for _, none := range [][]entities.PassedOverInstance{nil, {}} {
		if nobody, err := json.Marshal(PassedOverViewsOf(none)); err != nil || string(nobody) != `[]` {
			t.Errorf("a run that left nobody is written %s (%v), want an empty list", nobody, err)
		}
	}
}

// The approval of a migration answers with the plan through the same view
// the migrate route writes it through, and with each instance the run passed
// over as that route lists them: a client reads one shape from both.
func TestTheApprovalOfAMigrationAnswersInTheMigrateRoutesShapes(t *testing.T) {
	t.Parallel()
	plan := entities.MigrationPlan{SourceKey: "quotation", SourceVersion: 1, TargetVersion: 2, Instances: 1, RequiresSecondApprover: true,
		SecondApproverReasons: []string{"a reason"}}
	instance := uuid.Must(uuid.NewV7())
	result := entities.MigrationResult{Changed: 1, PassedOver: []entities.PassedOverInstance{{
		Instance: &entities.ProcessInstance{ID: instance}, Cause: entities.PassedOverLeftTheStep,
		Steps: []entities.PassedOverStep{{NodeID: "opsApprove", Name: "Operations approve"}}, Reason: "It had left.",
	}}}
	reply := approvalOf(entities.DeviationRequestOutcome{Request: wholeRequest(1), Applied: true, MigrationPlan: &plan, MigrationResult: &result})

	view, isView := reply.Plan.(MigrationPlanView)
	if !isView {
		t.Fatalf("the plan of an approved migration is a %T, want the view", reply.Plan)
	}
	if view.SourceKey != "quotation" || !view.RequiresSecondApprover || !slices.Equal(view.SecondApproverReasons, []string{"a reason"}) {
		t.Errorf("the plan of an approved migration: %+v", view)
	}
	want := []PassedOverView{{InstanceID: instance.String(), Cause: "left_the_step",
		Steps: []PassedOverStepView{{NodeID: "opsApprove", Name: "Operations approve"}}, Reason: "It had left."}}
	if got, _ := json.Marshal(reply.PassedOver); string(got) != mustJSON(t, want) {
		t.Errorf("passed_over is %s, want %s", got, mustJSON(t, want))
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return string(encoded)
}
