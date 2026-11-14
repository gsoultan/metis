package impl

import (
	"slices"
	"testing"
)

// A required field is missing when the completion leaves it absent, null or
// empty — the inbox's rule. false and 0 are answers. A field a condition can
// hide is never asked for, and neither is one that is not required.
func TestARequiredFieldIsMissingOnlyWhenItHasNoAnswer(t *testing.T) {
	form := `[
		{"id":"approved","type":"boolean","required":true},
		{"id":"amount","type":"number","required":true},
		{"id":"reason","type":"textarea","required":true,"logic":{"hiddenIf":"data.approved == true"}},
		{"id":"note","type":"textarea"},
		{"id":"","required":true},
		"not a field"
	]`
	fields := fieldsOf(inlineForm(form))

	for _, tc := range []struct {
		name string
		vars map[string]any
		want []string
	}{
		{"nothing sent, as the one-click Complete did", nil, []string{"amount", "approved"}},
		{"an empty completion", map[string]any{}, []string{"amount", "approved"}},
		{"false and 0 are answers", map[string]any{"approved": false, "amount": 0}, nil},
		{"null is no answer", map[string]any{"approved": nil, "amount": 5}, []string{"approved"}},
		{"the empty string is no answer", map[string]any{"approved": true, "amount": ""}, []string{"amount"}},
		{"a hidden-able field is not asked for", map[string]any{"approved": true, "amount": 1}, nil},
		{"a list is an answer", map[string]any{"approved": []any{}, "amount": map[string]any{}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := missingRequiredFields(fields, tc.vars)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("missing = %v, want %v", got, tc.want)
			}
		})
	}
}

// A form with no required field, or no form at all, asks for nothing: the
// rule adds a check only where the form's author asked for one.
func TestAFormWithoutRequiredFieldsAsksForNothing(t *testing.T) {
	for _, form := range []string{``, `not json`, `[{"id":"approved","type":"boolean"}]`, `{"fields":[{"id":"note"}]}`} {
		if got := missingRequiredFields(fieldsOf(inlineForm(form)), nil); len(got) != 0 {
			t.Fatalf("the form %q asked for %v", form, got)
		}
	}
}

// A field that appears twice, inline and in the stored form, is named once.
func TestAMissingFieldIsNamedOnce(t *testing.T) {
	fields := append(
		fieldsOf(inlineForm(`[{"id":"approved","required":true}]`)),
		fieldsOf(inlineForm(`{"fields":[{"id":"approved","required":true}]}`))...,
	)
	if got := missingRequiredFields(fields, nil); !slices.Equal(got, []string{"approved"}) {
		t.Fatalf("missing = %v, want [approved]", got)
	}
}
