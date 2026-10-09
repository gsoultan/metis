package impl

import (
	"maps"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/repositories/models"
)

// A mapping renames a step when the id it maps to is new to the source
// version, nothing else is mapped onto it, and the new version no longer has
// the step under its old id. Every other mapping redirects work, and finished
// work does not follow it.
func TestOnlyAMappingToANewIdThatNothingElseMapsOntoIsARename(t *testing.T) {
	t.Parallel()
	source := map[string]models.FlowNode{
		"submit": {ID: "submit"}, "opsApprove": {ID: "opsApprove"}, "salesApprove": {ID: "salesApprove"}, "sign": {ID: "sign"},
	}
	// The new version: submit is gone and request is new; one approval where
	// there were two is offered under a new id; sign is as it was, and so is
	// prepare — which the source never had under another id.
	target := map[string]models.FlowNode{
		"request": {ID: "request"}, "approve": {ID: "approve"}, "salesApprove": {ID: "salesApprove"}, "sign": {ID: "sign"},
		"file": {ID: "file"},
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
		// The new version still has sign, and adds file: sign is not file
		// under a new name. Whoever signed has not filed.
		{"a new id for a step the new version still has", map[string]string{"sign": "file"}, map[string]string{}},
		{
			"a rename beside a new id for a step the new version still has",
			map[string]string{"submit": "request", "sign": "file"},
			map[string]string{"submit": "request"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := renamedSteps(source, target, c.mapping); !maps.Equal(got, c.want) {
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
	// The new version: submit has become request; the rest is as it was.
	target := map[string]models.FlowNode{
		"prepare": source["prepare"], "opsApprove": source["opsApprove"], "control": source["control"], "request": {ID: "request"},
	}
	done := []models.ProcessInstanceModel{
		{Status: models.ProcessActive, CompletedNodes: []string{"start", "prepare", "submit"}},
		{Status: models.ProcessActive, CompletedNodes: []string{"start"}},
	}
	cases := []struct {
		name    string
		mapping map[string]string
		want    []string
	}{
		{"a redirect of a step an instance completed", map[string]string{"prepare": "control"},
			[]string{`"prepare" is mapped onto "control"`, `1 running instance(s) have completed "prepare"`}},
		{"a redirect of a control nobody has completed", map[string]string{"opsApprove": "control"},
			[]string{`0 running instance(s) have completed "opsApprove"`, `"opsApprove" carries a control obligation`}},
		{"a redirect of a step nobody completed and that carries nothing", map[string]string{"control": "prepare"}, nil},
		{"a rename of a step an instance completed", map[string]string{"submit": "request"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			warnings := redirectWarnings(source, target, c.mapping, nil, done)
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

// The number in the warning is of the instances the migration would move:
// running ones. An instance that finished on this version completed the step
// too, and is not the plan's to count.
func TestTheRedirectWarningCountsOnlyRunningInstances(t *testing.T) {
	t.Parallel()
	source := map[string]models.FlowNode{"prepare": {ID: "prepare"}, "control": {ID: "control"}}
	instances := []models.ProcessInstanceModel{
		{Status: models.ProcessActive, CompletedNodes: []string{"start", "prepare"}},
		{Status: models.ProcessCompleted, CompletedNodes: []string{"start", "prepare", "control"}},
		{Status: models.ProcessCancelled, CompletedNodes: []string{"start", "prepare"}},
	}
	warnings := redirectWarnings(source, source, map[string]string{"prepare": "control"}, nil, instances)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `1 running instance(s) have completed "prepare"`) {
		t.Fatalf("want the one warning counting the one running instance, got %v", warnings)
	}
	finishedOnly := redirectWarnings(source, source, map[string]string{"prepare": "control"}, nil, instances[1:])
	if len(finishedOnly) != 0 {
		t.Fatalf("no running instance completed the step and it carries no control; warned anyway: %v", finishedOnly)
	}
}

// A rename onto a step marked as a control is followed by finished work only
// when it is the control itself renamed where it stands. Any other is warned
// of as the redirect it is for work already done; the in-place one is not.
func TestARenameOntoAControlThatStandsElsewhereIsWarnedOfAsARedirect(t *testing.T) {
	t.Parallel()
	source := map[string]models.FlowNode{"prepare": {ID: "prepare"}, "sign": {ID: "sign"}}
	target := map[string]models.FlowNode{
		"sign": {ID: "sign"}, "audit": {ID: "audit", Properties: map[string]any{"compliance_relevant": true}},
	}
	prepared := []models.ProcessInstanceModel{{Status: models.ProcessActive, CompletedNodes: []string{"start", "prepare"}}}
	mapping := map[string]string{"prepare": "audit"}

	warnings := redirectWarnings(source, target, mapping, nil, prepared)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `"prepare" is mapped onto "audit", which is a different step`) ||
		!strings.Contains(warnings[0], `1 running instance(s) have completed "prepare"`) {
		t.Fatalf("a finished step renamed onto a control that stands elsewhere: %v, want the one warning", warnings)
	}
	if warnings := redirectWarnings(source, target, mapping, mapping, prepared); len(warnings) != 0 {
		t.Fatalf("the same rename in place: warned of %v", warnings)
	}
}
