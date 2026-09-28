package impl

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/models"
)

// A form declares the id of each of its fields, read the way the inbox reads a
// form: a list of fields, or an object holding the list under "fields".
// Anything that is not a form declares nothing.
func TestAFormDeclaresTheIDOfEachOfItsFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		form string
		want []string
	}{
		{"a list of fields, as the designer saves one", `[{"id":"approved","type":"boolean"},{"id":"reason"}]`, []string{"approved", "reason"}},
		{"an object holding the list, as a stored form is", `{"fields":[{"id":"approved"},{"id":"reason"}]}`, []string{"approved", "reason"}},
		{"a hidden field is still a field", `[{"id":"reason","logic":{"hiddenIf":"data.approved == true"}}]`, []string{"reason"}},
		{"a section has an id like any field", `[{"id":"heading","type":"section"},{"id":"approved"}]`, []string{"approved", "heading"}},
		{"a field with no id names nothing", `[{"label":"Approved"},{"id":""},{"id":7},{"id":"reason"}]`, []string{"reason"}},
		{"what is not a field is skipped", `["approved",42,null,{"id":"reason"}]`, []string{"reason"}},
		{"an object with no list of fields", `{"approved":{"type":"boolean"}}`, nil},
		{"fields that are not a list", `{"fields":{"id":"approved"}}`, nil},
		{"a scalar", `"approved"`, nil},
		{"null", `null`, nil},
		{"no form", ``, nil},
		{"blank", "  \n", nil},
		{"not JSON", `[{"id":"approved"`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			declared := map[string]struct{}{}
			addFieldIDs(declared, inlineForm(tc.form))
			got := make([]string, 0, len(declared))
			for id := range declared {
				got = append(got, id)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("the form %q declares %v, want %v", tc.form, got, tc.want)
			}
		})
	}
}

// The refusal says which variables were refused and why. The names are the
// caller's, so the list is sorted, at most maxNamesShown long, and each name
// is cut to maxNameShown characters.
func TestARefusalNamesWhatWasRefusedSortedAndBounded(t *testing.T) {
	withForm := models.TaskModel{FormDefinition: `[{"id":"approved"}]`}
	many := make(map[string]any, 12)
	for i := range 12 {
		many[fmt.Sprintf("v%02d", i)] = i
	}
	long := strings.Repeat("é", maxNameShown+36)

	for _, tc := range []struct {
		name string
		task models.TaskModel
		vars map[string]any
		want string
	}{
		{
			"one name",
			withForm, map[string]any{"approved": true, "amount": 1},
			"this task's form has no field named amount; a task can set only the variables its form declares",
		},
		{
			"several, sorted",
			withForm, map[string]any{"approved_by": "mallory", "amount": 1},
			"this task's form has no fields named amount, approved_by; a task can set only the variables its form declares",
		},
		{
			"no form at all",
			models.TaskModel{}, map[string]any{"amount": 1},
			"this task has no form to declare amount; a task can set only the variables its form declares",
		},
		{
			"a form key whose form is not stored here is a form all the same",
			models.TaskModel{FormKey: "embedded:app:forms/refund.html"}, map[string]any{"amount": 1},
			"this task's form has no field named amount; a task can set only the variables its form declares",
		},
		{
			"more than are shown",
			withForm, many,
			"this task's form has no fields named v00, v01, v02, v03, v04, v05, v06, v07, v08, v09 and 2 more; " +
				"a task can set only the variables its form declares",
		},
		{
			"a name longer than is shown, cut by character",
			withForm, map[string]any{long: 1},
			"this task's form has no field named " + strings.Repeat("é", maxNameShown) + "…; " +
				"a task can set only the variables its form declares",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			declared := map[string]struct{}{}
			addFieldIDs(declared, inlineForm(tc.task.FormDefinition))
			err := refuseUndeclared(tc.task, namesOutside(tc.vars, declared))
			if !errors.Is(err, apierr.ErrInvalidArgument) {
				t.Fatalf("got %v, want an invalid-argument refusal (a 400)", err)
			}
			if got := strings.TrimPrefix(err.Error(), apierr.ErrInvalidArgument.Error()+": "); got != tc.want {
				t.Fatalf("the refusal reads\n  %q\nwant\n  %q", got, tc.want)
			}
		})
	}
}

// What the check costs a completion whose variables an inline form declares:
// the common case, and the one every inbox submission takes.
func BenchmarkAdmittingVariablesAnInlineFormDeclares(b *testing.B) {
	task := models.TaskModel{FormDefinition: `[` +
		`{"id":"approved","label":"Approved","type":"boolean","required":true},` +
		`{"id":"reason","label":"Why not","type":"textarea","logic":{"hiddenIf":"data.approved == true"}},` +
		`{"id":"amount_checked","label":"Amount checked","type":"boolean"},` +
		`{"id":"cost_centre","label":"Cost centre","type":"select","options":[{"value":"ops","label":"Ops"}]},` +
		`{"id":"due","label":"Pay by","type":"date"},` +
		`{"id":"notes","label":"Notes","type":"textarea"}]`}
	vars := map[string]any{"approved": true, "reason": "", "amount_checked": true, "cost_centre": "ops", "due": "2026-10-01", "notes": ""}
	s := &taskService{}
	b.ReportAllocs()
	for b.Loop() {
		if err := s.admitVariables(b.Context(), task, vars); err != nil {
			b.Fatal(err)
		}
	}
}
