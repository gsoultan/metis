package impl

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

// TestCollectJavaScriptConditions pins what the worklist reports: exactly the
// fields the condition chain evaluates, with the evaluator's own prefix rule.
// A report that claimed more (timer expressions, script bodies) or less
// (nested flows, completion conditions) would misstate what turning the
// javascript-conditions flag on or off actually changes.
func TestCollectJavaScriptConditions(t *testing.T) {
	def := &entities.ProcessDefinition{
		ID:      uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		Key:     "claim-review",
		Name:    "Claim review",
		Version: 3,
		Nodes: []*entities.Node{
			{ID: "collect", Name: "Collect reviews", CompletionCondition: "js:reviewsDone >= 2"},
			{ID: "feel-done", Name: "FEEL is fine", CompletionCondition: "reviewsDone >= 2"},
			// Node.Condition carries timer expressions and script bodies, which
			// the flag does not gate — it must not be reported.
			{ID: "wait", Name: "Wait", Condition: "js:PT5M"},
			{
				ID: "sub", Name: "Subprocess",
				Nodes: []*entities.Node{
					{ID: "inner", Name: "Inner step", CompletionCondition: "js:innerDone"},
				},
				Flows: []*entities.SequenceFlow{
					{ID: "nested-flow", Condition: "js:x > 1"},
				},
			},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f-js", Condition: "js:amount > 100"},
			{ID: "f-feel", Condition: "amount > 100"},
			// The evaluator checks the prefix without trimming, so a leading
			// space is not JavaScript to it and must not be JavaScript here.
			{ID: "f-space", Condition: " js:amount > 100"},
			{ID: "f-empty"},
		},
	}

	got := collectJavaScriptConditions(def)

	want := map[string]string{
		"collect":     "completion condition",
		"inner":       "completion condition",
		"nested-flow": "flow condition",
		"f-js":        "flow condition",
	}
	if len(got) != len(want) {
		t.Fatalf("reported %d usages, want %d: %+v", len(got), len(want), got)
	}
	for _, u := range got {
		where, ok := want[u.ElementID]
		if !ok {
			t.Errorf("reported %q, which the flag does not gate", u.ElementID)
			continue
		}
		if u.Where != where {
			t.Errorf("element %q reported as %q, want %q", u.ElementID, u.Where, where)
		}
		if u.DefinitionKey != "claim-review" || u.Version != 3 {
			t.Errorf("element %q lost its definition context: %+v", u.ElementID, u)
		}
	}
}

// TestCollectJavaScriptConditionsFindsNothingInACleanDefinition pins the happy
// ending: a fully migrated definition contributes nothing to the worklist.
func TestCollectJavaScriptConditionsFindsNothingInACleanDefinition(t *testing.T) {
	def := &entities.ProcessDefinition{
		ID:  uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		Key: "clean",
		Nodes: []*entities.Node{
			{ID: "step", CompletionCondition: "done >= 2"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", Condition: `status = "GOLD"`},
		},
	}
	if got := collectJavaScriptConditions(def); len(got) != 0 {
		t.Fatalf("a clean definition produced a worklist: %+v", got)
	}
}
