package impl

import (
	"slices"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// Each way a run leaves an instance alone has a cause a client can translate,
// names its steps — by id, and by the name the source version gives them —
// and keeps the sentence the reply has always carried: the English a client
// falls back to.
func TestEveryWayOfBeingPassedOverHasACauseAndNamesItsSteps(t *testing.T) {
	source := models.ProcessDefinitionModel{Version: 1, Nodes: []models.FlowNode{
		{ID: "opsApprove", Name: "Operations approve"}, {ID: "wait"},
		{ID: "sub", Name: "Checks", Nodes: []models.FlowNode{{ID: "extraCheck", Name: "Extra check"}}},
	}}
	target := models.ProcessDefinitionModel{Version: 2}
	both := []string{"opsApprove", "wait"}
	// A step the version gives no name goes by its id, as it does in the
	// sentence.
	named := []entities.PassedOverStep{{NodeID: "opsApprove", Name: "Operations approve"}, {NodeID: "wait", Name: "wait"}}
	cases := []struct {
		why   leftAlone
		cause entities.PassedOverCause
		steps []entities.PassedOverStep
		says  string
	}{
		{becauseItLeftTheStep(source, "opsApprove"), entities.PassedOverLeftTheStep, named[:1], leftTheStep(source, "opsApprove")},
		{becauseItStopped(source), entities.PassedOverNoLongerRunning, nil, noLongerRunning(source)},
		{becauseNotPlannedFor(source), entities.PassedOverNotPlannedFor, nil, notPlannedFor(source)},
		{becauseAlreadyMoved(source), entities.PassedOverAlreadyMoved, nil, alreadyMoved(source)},
		{becauseNowhereToLand(source, target, both), entities.PassedOverNowhereToLand, named, nowhereToLand(source, target, both)},
		{becauseNothingDecidesThere(source, target, both), entities.PassedOverLeftWhereNothingDecides, named, leftWhereNothingDecides(source, target, both)},
		{becauseUndecided(source, both), entities.PassedOverWaitingToBeDecided, named, waitingToBeDecided(source, both)},
		{becauseCountersWouldMerge(source), entities.PassedOverCountersWouldMerge, nil, countersWouldMerge(source)},
	}
	given := map[entities.PassedOverCause]bool{}
	for _, c := range cases {
		if c.why.cause != c.cause || !c.cause.Valid() || !slices.Equal(c.why.steps, c.steps) || c.why.reason != c.says || c.why.none() {
			t.Errorf("%s: got cause %q steps %v reason %q", c.cause, c.why.cause, c.why.steps, c.why.reason)
		}
		if c.says == "" {
			t.Errorf("%s: the sentence beside the cause is empty", c.cause)
		}
		given[c.cause] = true
	}
	for _, cause := range entities.PassedOverCauses() {
		if !given[cause] {
			t.Errorf("no run ever gives the cause %q, and a client has words for it", cause)
		}
	}
	if len(given) != len(entities.PassedOverCauses()) {
		t.Errorf("a run gives %d causes and the closed set names %d", len(given), len(entities.PassedOverCauses()))
	}
	if !(leftAlone{}).none() || entities.PassedOverCause("moved").Valid() || entities.PassedOverCause("").Valid() {
		t.Error("an instance that may be moved has no cause, and the causes are a closed set")
	}
	told := passedOver(models.ProcessInstanceModel{}, becauseItLeftTheStep(source, "opsApprove"))
	if told.Cause != entities.PassedOverLeftTheStep || !slices.Equal(told.Steps, named[:1]) || told.Reason != leftTheStep(source, "opsApprove") {
		t.Errorf("the run's account of it: %+v", told)
	}
}

// A step is named as the sentence beside it names it: a step inside a
// sub-process by its own name, and one the version does not have by the id it
// was asked about.
func TestTheStepsOfACauseAreNamedAsTheSentenceNamesThem(t *testing.T) {
	source := namedSteps()
	got := stepsOf(source, []string{"extraCheck", "gate", "gone"})
	want := []entities.PassedOverStep{
		{NodeID: "extraCheck", Name: "Extra check"}, {NodeID: "gate", Name: "gate"}, {NodeID: "gone", Name: "gone"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("stepsOf = %+v, want %+v", got, want)
	}
	if steps := stepsOf(source, nil); steps != nil {
		t.Errorf("a cause about no step names %+v", steps)
	}
}
