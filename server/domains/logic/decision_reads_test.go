package logic

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic/feel"
)

func column(expression string) entities.DecisionInput {
	return entities.DecisionInput{Expression: expression}
}

func line(cells ...string) entities.DecisionRule {
	return entities.DecisionRule{Inputs: cells}
}

// What a decision table reads from the variables it is evaluated with: its
// columns, by the text of each as the evaluator looks it up; whatever a cell
// names beyond its own column; and the decisions it requires, by key.
func TestDecisionTableReads(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		table      entities.DecisionDefinition
		names      []string
		requires   []string
		analysable bool
	}{
		{name: "nothing in it", analysable: true},
		{
			name:       "a column is read by its text",
			table:      entities.DecisionDefinition{Inputs: []entities.DecisionInput{column("tier"), column("amount")}},
			names:      []string{"amount", "tier"},
			analysable: true,
		},
		{
			name: "a column that parses to more is read both ways",
			table: entities.DecisionDefinition{Inputs: []entities.DecisionInput{
				column("applicant.income"), column("price * quantity"),
			}},
			names:      []string{"applicant", "applicant.income", "price", "price * quantity", "quantity"},
			analysable: true,
		},
		{
			// The evaluator never parses a column: it looks the text up as one
			// variable's name, so text FEEL cannot read is still fully known.
			name:       "a column FEEL cannot parse is the variable of that name",
			table:      entities.DecisionDefinition{Inputs: []entities.DecisionInput{column("first name")}},
			names:      []string{"first name"},
			analysable: true,
		},
		{
			// The evaluator looks up whatever the column says, and a column that
			// says nothing looks up the variable whose name is empty.
			name:       "a column with no expression reads the variable with no name",
			table:      entities.DecisionDefinition{Inputs: []entities.DecisionInput{column("amount"), column("")}},
			names:      []string{"", "amount"},
			analysable: true,
		},
		{
			name: "a cell reads a variable no column names",
			table: entities.DecisionDefinition{
				Inputs: []entities.DecisionInput{column("amount")},
				Rules:  []entities.DecisionRule{line("> credit_limit"), line("< date(cutoff)"), line("[floor..ceiling]")},
			},
			names:      []string{"amount", "ceiling", "credit_limit", "cutoff", "floor"},
			analysable: true,
		},
		{
			name: "a lone word in a cell is text, unless it names a column",
			table: entities.DecisionDefinition{
				Inputs: []entities.DecisionInput{column("tier"), column("minimum")},
				Rules: []entities.DecisionRule{
					line("GOLD", "-"), line(`"GOLD","SILVER"`, ""), line("not(blocked, suspended)", "minimum"),
				},
			},
			names:      []string{"minimum", "tier"},
			analysable: true,
		},
		{
			name: "with an operator a word is a variable, and _input is the column itself",
			table: entities.DecisionDefinition{
				Inputs: []entities.DecisionInput{column("amount")},
				Rules:  []entities.DecisionRule{line("= manager"), line("> _input - margin"), line("not(> ceiling)")},
			},
			names:      []string{"amount", "ceiling", "manager", "margin"},
			analysable: true,
		},
		{
			name: "a cell past the last column is never tested",
			table: entities.DecisionDefinition{
				Inputs: []entities.DecisionInput{column("amount")},
				Rules:  []entities.DecisionRule{line("> 1", "> hidden"), line("> 2", "> >")},
			},
			names:      []string{"amount"},
			analysable: true,
		},
		{
			name: "a cell that cannot be read makes the table not analysable",
			table: entities.DecisionDefinition{
				Inputs: []entities.DecisionInput{column("amount"), column("tier")},
				Rules:  []entities.DecisionRule{line("> credit_limit", "GOLD"), line("> >", "GOLD")},
			},
			names:      []string{"amount", "credit_limit", "tier"},
			analysable: false,
		},
		{
			name: "the decisions it requires, each once",
			table: entities.DecisionDefinition{
				Inputs:            []entities.DecisionInput{column("risk")},
				RequiredDecisions: []string{"risk", "limits", "risk", ""},
			},
			names:      []string{"risk"},
			requires:   []string{"limits", "risk"},
			analysable: true,
		},
	}
	for _, c := range cases {
		names, requires, analysable := DecisionTableReads(c.table)
		if analysable != c.analysable || !reflect.DeepEqual(names, c.names) || !reflect.DeepEqual(requires, c.requires) {
			t.Errorf("%s: got %v, %v, %v; want %v, %v, %v", c.name, names, requires, analysable, c.names, c.requires, c.analysable)
		}
	}
}

// What a step's input mapping reads: each source is looked up as a variable's
// name and, failing that, evaluated as FEEL (mapping.Resolve).
func TestMappingSourceNames(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		mapping    map[string]any
		names      []string
		analysable bool
	}{
		{"no mapping", nil, nil, true},
		{"a rename", map[string]any{"ok": "approved"}, []string{"approved"}, true},
		{"arithmetic", map[string]any{"total": "price * quantity"}, []string{"price", "price * quantity", "quantity"}, true},
		{"a path", map[string]any{"country": "customer.country"}, []string{"customer", "customer.country"}, true},
		{"a constant reads nothing", map[string]any{"flag": true, "limit": 5.0}, nil, true},
		// mapping.Resolve looks the text up before anything else, whatever it is:
		// a source left blank reads the variable of that name, and no more.
		{"a blank source reads the variable of that name", map[string]any{"unset": "", "blank": "   "}, []string{"", "   "}, true},
		{"several sources", map[string]any{"a": "approved", "b": "sum(items.price)", "c": "approved"}, []string{"approved", "items", "sum(items.price)"}, true},
		{"a source that cannot be read", map[string]any{"ok": "approved", "broken": "price *"}, []string{"approved", "price *"}, false},
	}
	for _, c := range cases {
		names, analysable := MappingSourceNames(c.mapping)
		if analysable != c.analysable || !reflect.DeepEqual(names, c.names) {
			t.Errorf("%s: got %v, %v; want %v, %v", c.name, names, analysable, c.names, c.analysable)
		}
	}
}

// A cell is evaluated two levels down — the list of tests, then the test — so
// its expression has two levels fewer than a condition before the evaluator
// refuses it, and the names in it are read exactly as far.
func TestACellIsReadAsDeepAsTheEvaluatorEvaluatesIt(t *testing.T) {
	t.Parallel()
	vars := map[string]any{"a": 1.0}
	refusedSomewhere, readSomewhere := false, false
	for terms := 120; terms <= 136; terms++ {
		cell := "> " + strings.Repeat("a+", terms-1) + "a"
		_, err := feel.EvaluateUnaryTests(cell, 1.0, vars, []string{"amount"})
		refused := err != nil && strings.Contains(err.Error(), "too deeply nested")
		_, _, analysable := DecisionTableReads(entities.DecisionDefinition{
			Inputs: []entities.DecisionInput{column("amount")}, Rules: []entities.DecisionRule{line(cell)},
		})
		if analysable == refused {
			t.Errorf("%d terms: the evaluator says %v and the table is analysable: %v", terms, err, analysable)
		}
		refusedSomewhere, readSomewhere = refusedSomewhere || refused, readSomewhere || !refused
	}
	if !refusedSomewhere || !readSomewhere {
		t.Error("the evaluator's limit is not between 120 and 136 terms; this test no longer straddles it")
	}
}
