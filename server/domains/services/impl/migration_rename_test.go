package impl

import (
	"maps"
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
