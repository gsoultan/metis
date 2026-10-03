package impl

import (
	"testing"

	"github.com/gsoultan/metis/server/repositories/models"
)

// The name a decision's row carries is the step's own, found anywhere in the
// version's graph, and the step's id where it has no name.
func TestADecisionsStepIsNamedFromTheGraph(t *testing.T) {
	t.Parallel()
	nodes := []models.FlowNode{
		{ID: "start"},
		{ID: "review", Name: "Review", Nodes: []models.FlowNode{
			{ID: "opsApprove", Name: "Operations approve"},
			{ID: "unnamed"},
		}},
	}
	for id, want := range map[string]string{
		"review": "Review", "opsApprove": "Operations approve", "unnamed": "unnamed", "gone": "gone",
	} {
		if got := nodeNameIn(nodes, id); got != want {
			t.Errorf("step %q is named %q, want %q", id, got, want)
		}
	}
}
