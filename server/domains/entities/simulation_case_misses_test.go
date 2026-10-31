package entities

import "testing"

func TestAnExpectationNamesWhatARunGotWrong(t *testing.T) {
	run := SimulationRun{Outcome: SimulationOutcomeCompleted, EndedAtNode: "rejected",
		Variables: map[string]any{"limit": 4200.0, "who": map[string]any{"b": 1, "a": 2}}}

	pass := SimulationExpectation{Outcome: SimulationOutcomeCompleted, EndsAt: "rejected",
		Variables: map[string]any{"limit": 4200, "who": map[string]any{"a": 2, "b": 1}}}
	if got := pass.Misses(run); len(got) != 0 {
		t.Fatalf("a run that is what the case expects missed %v", got)
	}

	fail := SimulationExpectation{Outcome: SimulationOutcomeIncident, EndsAt: "approved",
		Variables: map[string]any{"limit": 100, "missing": true}}
	if got := fail.Misses(run); len(got) != 4 {
		t.Fatalf("got %d misses %v, want the outcome, the end, a wrong value and an unset one", len(got), got)
	}
	if got := (SimulationExpectation{}).Misses(run); len(got) != 0 {
		t.Fatalf("an expectation of nothing missed %v", got)
	}
}
