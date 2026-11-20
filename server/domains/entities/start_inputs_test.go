package entities

import (
	"strings"
	"testing"
)

func expenseInputs(t *testing.T) []StartInput {
	t.Helper()
	inputs, err := ParseStartInputs([]any{
		map[string]any{"id": "heading", "type": "section", "label": "The claim"},
		map[string]any{"id": "amount", "type": "number", "required": true},
		map[string]any{"id": "currency", "type": "select", "defaultValue": "GBP", "options": []any{
			map[string]any{"value": "GBP", "label": "Pounds"}, map[string]any{"value": "EUR", "label": "Euros"},
		}},
		map[string]any{"id": "description", "type": "text"},
		map[string]any{"id": "urgent", "type": "boolean"},
		map[string]any{"id": "spent_on", "type": "date"},
	})
	if err != nil {
		t.Fatalf("the inputs were refused: %v", err)
	}
	return inputs
}

func TestAStartThatGivesEveryInputIsAdmittedWithItsDefaults(t *testing.T) {
	given := map[string]any{"amount": 12.5, "note": "not declared"}
	admitted, problems := AdmitStartInputs(expenseInputs(t), given)
	if len(problems) != 0 {
		t.Fatalf("a start that gave the required input was refused: %v", problems)
	}
	if admitted["currency"] != "GBP" {
		t.Fatalf("the input left out did not take its default: %v", admitted["currency"])
	}
	if admitted["note"] != "not declared" {
		t.Fatal("a variable the start step does not declare was dropped")
	}
	if _, changed := given["currency"]; changed {
		t.Fatal("the caller's own variables were changed")
	}
}

func TestAStartIsRefusedForEachInputThatIsMissingOrTheWrongKind(t *testing.T) {
	for _, tc := range []struct {
		name string
		vars map[string]any
		want string
	}{
		{"nothing given", nil, "amount is required"},
		{"required given as nothing", map[string]any{"amount": nil}, "amount is required"},
		{"required given as empty text", map[string]any{"amount": "  "}, "amount is required"},
		{"a number as text", map[string]any{"amount": "12"}, "amount must be a number"},
		{"a choice not offered", map[string]any{"amount": 1, "currency": "USD"}, "currency must be one of GBP, EUR"},
		{"text as a number", map[string]any{"amount": 1, "description": 7}, "description must be text"},
		{"yes or no as text", map[string]any{"amount": 1, "urgent": "yes"}, "urgent must be true or false"},
		{"a date that is not one", map[string]any{"amount": 1, "spent_on": "last week"}, "spent_on must be a date"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			admitted, problems := AdmitStartInputs(expenseInputs(t), tc.vars)
			if admitted != nil {
				t.Fatal("a refused start was still given variables to start with")
			}
			if len(problems) != 1 || !strings.Contains(problems[0], tc.want) {
				t.Fatalf("problems = %v, want one saying %q", problems, tc.want)
			}
		})
	}
}

func TestEveryWrongInputIsNamedAtOnce(t *testing.T) {
	_, problems := AdmitStartInputs(expenseInputs(t), map[string]any{"urgent": 1, "spent_on": 3})
	if len(problems) != 3 {
		t.Fatalf("three inputs were wrong, %d were named: %v", len(problems), problems)
	}
}

func TestValuesThatAreNotNothingAreTaken(t *testing.T) {
	vars := map[string]any{"amount": 0, "urgent": false, "spent_on": "2026-01-31T09:00:00Z"}
	if _, problems := AdmitStartInputs(expenseInputs(t), vars); len(problems) != 0 {
		t.Fatalf("zero, false and an instant were refused: %v", problems)
	}
}

func TestAProcessThatDeclaresNoInputsStartsWithAnything(t *testing.T) {
	inputs, err := StartInputsOf(&Node{ID: "start", Type: StartEvent})
	if err != nil || inputs != nil {
		t.Fatalf("a start step with no form declared %v (%v)", inputs, err)
	}
	vars := map[string]any{"anything": []any{1, "two"}}
	admitted, problems := AdmitStartInputs(inputs, vars)
	if len(problems) != 0 || len(admitted) != 1 {
		t.Fatalf("a start with no declared inputs was changed or refused: %v %v", admitted, problems)
	}
}

func TestAFieldTheFormShowsOnlySometimesIsNotRequired(t *testing.T) {
	inputs, err := ParseStartInputs(`{"fields":[{"id":"reason","type":"text","required":true,"logic":{"hiddenIf":"amount < 100"}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, problems := AdmitStartInputs(inputs, nil); len(problems) != 0 {
		t.Fatalf("a field the form may never have shown was required: %v", problems)
	}
	if missing := MissingDefault(inputs); len(missing) != 0 {
		t.Fatalf("a conditional field was held against a schedule: %v", missing)
	}
}

func TestAChoiceWhoseOptionsComeFromElsewhereTakesAnyText(t *testing.T) {
	inputs, err := ParseStartInputs([]any{map[string]any{
		"id": "cost_centre", "type": "select", "dataSource": map[string]any{"type": "endpoint"},
		"options": []any{map[string]any{"value": "stale"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, problems := AdmitStartInputs(inputs, map[string]any{"cost_centre": "CC-9"}); len(problems) != 0 {
		t.Fatalf("a choice the engine cannot list was held to a list: %v", problems)
	}
}

func TestInputsTheEngineCannotHoldAStartToAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		form any
		want string
	}{
		{"not JSON", "{", "not JSON"},
		{"not a list", map[string]any{"title": "x"}, "not a list of fields"},
		{"a field that is not one", []any{"amount"}, "input 1 is not a field"},
		{"no name", []any{map[string]any{"type": "text"}}, "input 1 has no name"},
		{"the same name twice", []any{map[string]any{"id": "a", "type": "text"}, map[string]any{"id": "a", "type": "number"}}, `"a" is declared twice`},
		{"a default of the wrong kind", []any{map[string]any{"id": "amount", "type": "number", "defaultValue": "ten"}}, `default of input "amount" must be a number`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseStartInputs(tc.form)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one saying %q", err, tc.want)
			}
		})
	}
}

func TestMoreInputsThanTheBoundAreRefused(t *testing.T) {
	fields := make([]any, maxStartInputs+1)
	for i := range fields {
		fields[i] = map[string]any{"id": strings.Repeat("x", i+1), "type": "text"}
	}
	if _, err := ParseStartInputs(fields); err == nil {
		t.Fatal("a start step with more inputs than the bound was read")
	}
}
