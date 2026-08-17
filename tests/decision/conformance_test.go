package decision_test

import (
	"reflect"
	"testing"

	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/domains/services/impl"
)

// The DMN conformance corpus.
//
// This is the exit criterion for the decision engine: a table authored in
// another DMN tool — Camunda is the one people arrive from — must decide the
// same thing here. "The same thing" is not a matter of taste, it is what the
// specification says each hit policy and each unary test means, so the corpus
// is written as tables plus inputs plus the answer the specification requires.
//
// Every case here is one an author can reach from the editor. Cases that only a
// hand-written definition could produce belong in the unit tests next to the
// code; this file is about tables real people export and import.

type conformanceCase struct {
	name  string
	table entities.DecisionDefinition
	input map[string]any
	// want is the expected value map. A nil want means "no line matched".
	want map[string]any
	// wantErr is set for the cases where refusing is the correct answer.
	wantErr bool
}

// discountTable is the shape most DMN tutorials start from: two conditions, one
// result, overlapping lines. Only the hit policy changes between cases.
func discountTable(policy, aggregation string, values []string) entities.DecisionDefinition {
	return entities.DecisionDefinition{
		Key:         "discount",
		HitPolicy:   policy,
		Aggregation: aggregation,
		Inputs: []entities.DecisionInput{
			{ID: "i1", Label: "Order total", Expression: "total", Type: "number"},
			{ID: "i2", Label: "Customer tier", Expression: "tier", Type: "string"},
		},
		Outputs: []entities.DecisionOutput{
			{ID: "o1", Label: "Discount", Name: "discount", Type: "string", Values: values},
		},
		Rules: []entities.DecisionRule{
			{Inputs: []string{"-", "GOLD"}, Outputs: []any{"GOLD_RATE"}},
			{Inputs: []string{">= 100", "-"}, Outputs: []any{"BULK_RATE"}},
			{Inputs: []string{">= 1000", "-"}, Outputs: []any{"WHOLESALE_RATE"}},
		},
	}
}

func conformanceCases() []conformanceCase {
	// An order of 500 from a GOLD customer matches the first two lines and not
	// the third — the overlap every hit policy answers differently.
	overlapping := map[string]any{"total": 500.0, "tier": "GOLD"}
	ranking := []string{"WHOLESALE_RATE", "BULK_RATE", "GOLD_RATE"}

	return []conformanceCase{
		{
			name:  "FIRST takes the line written first",
			table: discountTable(entities.HitPolicyFirst, "", nil),
			input: overlapping,
			want:  map[string]any{"discount": "GOLD_RATE"},
		},
		{
			name:    "UNIQUE refuses an overlap",
			table:   discountTable(entities.HitPolicyUnique, "", nil),
			input:   overlapping,
			wantErr: true,
		},
		{
			name:  "UNIQUE is content when exactly one line matches",
			table: discountTable(entities.HitPolicyUnique, "", nil),
			input: map[string]any{"total": 10.0, "tier": "GOLD"},
			want:  map[string]any{"discount": "GOLD_RATE"},
		},
		{
			name:    "ANY refuses lines that disagree",
			table:   discountTable(entities.HitPolicyAny, "", nil),
			input:   overlapping,
			wantErr: true,
		},
		{
			name:  "PRIORITY takes the highest-ranked result, not the first line",
			table: discountTable(entities.HitPolicyPriority, "", ranking),
			input: overlapping,
			want:  map[string]any{"discount": "BULK_RATE"},
		},
		{
			name:    "PRIORITY refuses a table with nothing to rank by",
			table:   discountTable(entities.HitPolicyPriority, "", nil),
			input:   overlapping,
			wantErr: true,
		},
		{
			name:  "OUTPUT ORDER returns every match, ranked",
			table: discountTable(entities.HitPolicyOutputOrder, "", ranking),
			input: overlapping,
			want:  map[string]any{"discount": []any{"BULK_RATE", "GOLD_RATE"}},
		},
		{
			name:  "RULE ORDER returns every match, as written",
			table: discountTable(entities.HitPolicyRuleOrder, "", nil),
			input: overlapping,
			want:  map[string]any{"discount": []any{"GOLD_RATE", "BULK_RATE"}},
		},
		{
			name:  "COLLECT returns every match",
			table: discountTable(entities.HitPolicyCollect, "", nil),
			input: overlapping,
			want:  map[string]any{"discount": []any{"GOLD_RATE", "BULK_RATE"}},
		},
		{
			name:  "no line matching is an empty answer, not a failure",
			table: discountTable(entities.HitPolicyFirst, "", nil),
			input: map[string]any{"total": 10.0, "tier": "BRONZE"},
			want:  map[string]any{},
		},
		{
			name:    "an unknown hit policy is refused rather than guessed at",
			table:   discountTable("SOMETHING_ELSE", "", nil),
			input:   overlapping,
			wantErr: true,
		},

		// Aggregations. COLLECT with a summary is how a DMN table adds up a
		// list of charges, and each function has to reduce the same match set.
		{
			name:  "COLLECT SUM adds the matches up",
			table: feeTable(entities.AggregationSum),
			input: map[string]any{"weight": 30.0},
			want:  map[string]any{"fee": 45.0},
		},
		{
			name:  "COLLECT MIN takes the smallest",
			table: feeTable(entities.AggregationMin),
			input: map[string]any{"weight": 30.0},
			want:  map[string]any{"fee": 20.0},
		},
		{
			name:  "COLLECT MAX takes the largest",
			table: feeTable(entities.AggregationMax),
			input: map[string]any{"weight": 30.0},
			want:  map[string]any{"fee": 25.0},
		},
		{
			name:  "COLLECT COUNT counts them",
			table: feeTable(entities.AggregationCount),
			input: map[string]any{"weight": 30.0},
			want:  map[string]any{"fee": 2.0},
		},

		// Unary tests. These are the cell notations a Camunda table uses, and
		// each one is a way a table silently stops matching if it is read wrong.
		{name: "closed range includes both ends", table: unaryTable("[10..20]"), input: num(10), want: yes()},
		{name: "closed range includes the top", table: unaryTable("[10..20]"), input: num(20), want: yes()},
		{name: "closed range excludes outside", table: unaryTable("[10..20]"), input: num(21), want: map[string]any{}},
		{name: "open low end excludes it", table: unaryTable("]10..20]"), input: num(10), want: map[string]any{}},
		{name: "open high end excludes it", table: unaryTable("[10..20["), input: num(20), want: map[string]any{}},
		{name: "greater than", table: unaryTable("> 10"), input: num(11), want: yes()},
		{name: "greater or equal", table: unaryTable(">= 10"), input: num(10), want: yes()},
		{name: "less than", table: unaryTable("< 10"), input: num(9), want: yes()},
		{name: "not equal", table: unaryTable("!= 10"), input: num(11), want: yes()},
		{name: "a bare number is equality", table: unaryTable("10"), input: num(10), want: yes()},
		{name: "a list matches any member", table: unaryTable("10, 20, 30"), input: num(20), want: yes()},
		{name: "a list misses a non-member", table: unaryTable("10, 20, 30"), input: num(25), want: map[string]any{}},
		{name: "negation", table: unaryTable("not(10)"), input: num(11), want: yes()},
		{name: "a dash matches anything", table: unaryTable("-"), input: num(-99), want: yes()},
		{name: "an empty cell matches anything", table: unaryTable(""), input: num(-99), want: yes()},

		// Strings, including the two deviations this engine documents: a bare
		// word in a cell is text, and single quotes are accepted.
		{name: "a quoted string matches", table: unaryTextTable(`"GOLD"`), input: text("GOLD"), want: yes()},
		{name: "a bare word in a cell is text", table: unaryTextTable("GOLD"), input: text("GOLD"), want: yes()},
		{name: "single quotes are accepted", table: unaryTextTable("'GOLD'"), input: text("GOLD"), want: yes()},
		{name: "a string comparison is exact", table: unaryTextTable(`"GOLD"`), input: text("gold"), want: map[string]any{}},
		{name: "a string list", table: unaryTextTable(`"GOLD", "SILVER"`), input: text("SILVER"), want: yes()},
		{name: "string negation", table: unaryTextTable(`not("GOLD")`), input: text("SILVER"), want: yes()},

		// Booleans.
		{name: "true matches true", table: unaryBoolTable("true"), input: map[string]any{"flag": true}, want: yes()},
		{name: "true does not match false", table: unaryBoolTable("true"), input: map[string]any{"flag": false}, want: map[string]any{}},
		{name: "false matches false", table: unaryBoolTable("false"), input: map[string]any{"flag": false}, want: yes()},

		// A type the cell cannot compare against yields no match rather than an
		// error: DMN says an incomparable comparison is null, and a table that
		// fails an instance because one variable arrived as text is worse than
		// one that does not match.
		{
			name:  "a mismatched type does not match and does not fail",
			table: unaryTable("> 10"),
			input: map[string]any{"value": "not a number"},
			want:  map[string]any{},
		},
		{
			name:  "a missing variable does not match and does not fail",
			table: unaryTable("> 10"),
			input: map[string]any{},
			want:  map[string]any{},
		},
	}
}

// feeTable has two lines that both match a weight of 30, producing 20 and 25.
func feeTable(aggregation string) entities.DecisionDefinition {
	return entities.DecisionDefinition{
		Key:         "fee",
		HitPolicy:   entities.HitPolicyCollect,
		Aggregation: aggregation,
		Inputs:      []entities.DecisionInput{{ID: "i1", Expression: "weight", Type: "number"}},
		Outputs:     []entities.DecisionOutput{{ID: "o1", Name: "fee", Type: "number"}},
		Rules: []entities.DecisionRule{
			{Inputs: []string{"> 10"}, Outputs: []any{20}},
			{Inputs: []string{"> 20"}, Outputs: []any{25}},
			{Inputs: []string{"> 100"}, Outputs: []any{99}},
		},
	}
}

// unaryTable is one line whose only condition is the cell under test.
func unaryTable(cell string) entities.DecisionDefinition {
	return oneCellTable(cell, "value", "number")
}

func unaryTextTable(cell string) entities.DecisionDefinition {
	return oneCellTable(cell, "value", "string")
}

func unaryBoolTable(cell string) entities.DecisionDefinition {
	return oneCellTable(cell, "flag", "boolean")
}

func oneCellTable(cell, variable, kind string) entities.DecisionDefinition {
	return entities.DecisionDefinition{
		Key:       "unary",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "i1", Expression: variable, Type: kind}},
		Outputs:   []entities.DecisionOutput{{ID: "o1", Name: "matched", Type: "boolean"}},
		Rules:     []entities.DecisionRule{{Inputs: []string{cell}, Outputs: []any{true}}},
	}
}

func num(v float64) map[string]any { return map[string]any{"value": v} }
func text(v string) map[string]any { return map[string]any{"value": v} }
func yes() map[string]any          { return map[string]any{"matched": true} }

func TestDMNConformance(t *testing.T) {
	evaluator := impl.NewDecisionTableEvaluator(impl.NewFEELEvaluator())

	for _, testCase := range conformanceCases() {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := evaluator.EvaluateTable(t.Context(), testCase.table, testCase.input)

			if testCase.wantErr {
				if err == nil {
					t.Fatalf("want a refusal, got %v", result.Values)
				}
				return
			}
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if !reflect.DeepEqual(result.Values, testCase.want) {
				t.Errorf("values = %#v, want %#v", result.Values, testCase.want)
			}
		})
	}
}

// The evaluator the engine actually reaches for must be the one the corpus
// tests. It was not always: a second copy of the hit-policy logic lived on the
// decision service, unreachable and free to drift.
var _ contracts.DecisionTableEvaluator = impl.NewDecisionTableEvaluator(impl.NewFEELEvaluator())
