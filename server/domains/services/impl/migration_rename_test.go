package impl

import (
	"maps"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/repositories/models"
)

// A mapping renames a step when the id it maps to is new to the source version
// and nothing else is mapped onto it. Every other mapping redirects work, and
// finished work does not follow it.
func TestOnlyAMappingToANewIdThatNothingElseMapsOntoIsARename(t *testing.T) {
	t.Parallel()
	source := map[string]models.FlowNode{
		"submit": {ID: "submit"}, "opsApprove": {ID: "opsApprove"}, "salesApprove": {ID: "salesApprove"}, "sign": {ID: "sign"},
	}
	cases := []struct {
		name    string
		mapping map[string]string
		want    map[string]string
	}{
		{"a new id", map[string]string{"submit": "request"}, map[string]string{"submit": "request"}},
		{"a step the source version has too", map[string]string{"opsApprove": "salesApprove"}, map[string]string{}},
		{"two steps onto one new id", map[string]string{"opsApprove": "approve", "salesApprove": "approve"}, map[string]string{}},
		{"a step onto itself", map[string]string{"sign": "sign"}, map[string]string{}},
		{
			"a rename beside a redirect",
			map[string]string{"submit": "request", "opsApprove": "salesApprove"},
			map[string]string{"submit": "request"},
		},
		{"no mapping", nil, map[string]string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := renamedSteps(source, c.mapping); !maps.Equal(got, c.want) {
				t.Errorf("renamedSteps(%v) = %v, want %v", c.mapping, got, c.want)
			}
		})
	}
}

// A redirect is warned of only where it says something about work already
// done: an instance in the plan completed the step, or the step carries a
// control. A rename never is.
func TestARedirectIsWarnedOfWhereWorkDoneOnTheStepWouldNotCount(t *testing.T) {
	t.Parallel()
	source := map[string]models.FlowNode{
		"prepare":    {ID: "prepare"},
		"opsApprove": {ID: "opsApprove", Properties: map[string]any{"compliance_relevant": true}},
		"control":    {ID: "control"},
		"submit":     {ID: "submit"},
	}
	done := []models.ProcessInstanceModel{{CompletedNodes: []string{"start", "prepare", "submit"}}, {CompletedNodes: []string{"start"}}}
	cases := []struct {
		name    string
		mapping map[string]string
		want    []string
	}{
		{"a redirect of a step an instance completed", map[string]string{"prepare": "control"},
			[]string{`"prepare" is mapped onto "control"`, `1 instance(s) have completed "prepare"`}},
		{"a redirect of a control nobody has completed", map[string]string{"opsApprove": "control"},
			[]string{`0 instance(s) have completed "opsApprove"`, `"opsApprove" carries a control obligation`}},
		{"a redirect of a step nobody completed and that carries nothing", map[string]string{"control": "prepare"}, nil},
		{"a rename of a step an instance completed", map[string]string{"submit": "request"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			warnings := redirectWarnings(source, c.mapping, done)
			if len(c.want) == 0 {
				if len(warnings) != 0 {
					t.Fatalf("warned of nothing worth warning: %v", warnings)
				}
				return
			}
			if len(warnings) != 1 {
				t.Fatalf("want the one warning, got %v", warnings)
			}
			for _, want := range c.want {
				if !strings.Contains(warnings[0], want) {
					t.Errorf("the warning does not say %q: %s", want, warnings[0])
				}
			}
		})
	}
}
