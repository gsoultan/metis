package impl

import (
	"context"
	"fmt"
	"reflect"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

// Running a table against its own examples.
//
// A decision table nobody can test is a spreadsheet with extra steps. The table
// is business policy, it changes often, and the person changing it is rarely the
// person who knows every case it was written for — so the cases live beside it
// and are re-run whenever somebody looks.
//
// The examples are stored with the table rather than in a test harness, because
// they are part of the policy: "an order over 10,000 from a new supplier goes to
// compliance" is as much a statement of the rule as any line in the grid.

// RunDecisionTests evaluates a table against every example stored with it.
func (s *decisionService) RunDecisionTests(ctx context.Context, id uuid.UUID) ([]entities.DecisionTestResult, error) {
	decision, err := s.GetDecision(ctx, id)
	if err != nil {
		return nil, err
	}

	results := make([]entities.DecisionTestResult, 0, len(decision.Tests))
	for _, example := range decision.Tests {
		results = append(results, s.runOneTest(ctx, decision, example))
	}
	return results, nil
}

func (s *decisionService) runOneTest(
	ctx context.Context,
	decision entities.DecisionDefinition,
	example entities.DecisionTest,
) entities.DecisionTestResult {
	result := entities.DecisionTestResult{ID: example.ID, Name: example.Name}

	// The table as it stands is evaluated directly rather than looked up by
	// key: an example is run against the version in front of the author,
	// including edits not yet saved when the caller passes one.
	outcome, err := s.tableEvaluator.EvaluateTable(ctx, decision, copyVariables(example.Inputs))
	if err != nil {
		// A table that cannot be evaluated at all is a different failure from
		// one that decides the wrong thing, and the difference matters: the
		// first is a broken table, the second is a disagreement about policy.
		result.Err = err.Error()
		return result
	}

	result.Actual = outcome.Values
	result.MatchedRules = outcome.MatchedRules
	result.Mismatches = compareExpected(example.Expected, outcome.Values)
	result.Passed = len(result.Mismatches) == 0
	return result
}

// compareExpected names the outputs that came back wrong.
//
// Only the outputs the example names are checked, so a test can pin the one
// value it cares about and stay silent about the rest — which is what lets a
// table grow a column without invalidating every example written before it.
func compareExpected(expected, actual map[string]any) []string {
	var mismatches []string
	for name, want := range expected {
		got, present := actual[name]
		if !present {
			mismatches = append(mismatches, fmt.Sprintf("%s: expected %v, but the table decided nothing", name, want))
			continue
		}
		if !valuesMatch(want, got) {
			mismatches = append(mismatches, fmt.Sprintf("%s: expected %v, got %v", name, want, got))
		}
	}
	return mismatches
}

// valuesMatch compares an expectation with a result.
//
// Numbers are compared as numbers. An expectation arrives through JSON as a
// float64 whatever the author typed, and a table's output may be an int from a
// hand-written definition — comparing those with == would fail a passing test
// and send somebody looking for a bug in their policy.
func valuesMatch(want, got any) bool {
	if wantNum, wantOK := numericOutput(want); wantOK {
		if gotNum, gotOK := numericOutput(got); gotOK {
			return wantNum == gotNum
		}
	}
	return reflect.DeepEqual(want, got)
}

// copyVariables keeps an example from being changed by the evaluation it feeds.
func copyVariables(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
