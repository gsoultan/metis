package impl

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A queued service call is kept from being made only for a step withdrawn with
// the ad-hoc sub-process around it, so "which ad-hoc sub-process is this step
// inside" has to be answered for both shapes a definition stores a sub-process
// in, at any depth — and answered "none" for everything else.
func TestEnclosingAdHocFindsTheNearestAdHocSubProcessInEitherShape(t *testing.T) {
	def := &entities.ProcessDefinition{
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			// Nested, children naming their parent.
			{ID: "research", Type: entities.SubProcess, IsAdHoc: true, Nodes: []*entities.Node{
				{ID: "notify", Type: entities.ServiceTask, ParentID: "research"},
				// Nested, children naming nothing.
				{ID: "gather", Type: entities.SubProcess, Nodes: []*entities.Node{
					{ID: "fetch", Type: entities.ServiceTask},
					{ID: "inner-research", Type: entities.SubProcess, IsAdHoc: true, Nodes: []*entities.Node{
						{ID: "inner-notify", Type: entities.ServiceTask},
					}},
				}},
			}},
			// Flat: beside their container, naming it.
			{ID: "review", Type: entities.SubProcess, IsAdHoc: true},
			{ID: "score", Type: entities.ServiceTask, ParentID: "review"},
			// A sub-process that is not ad-hoc.
			{ID: "pack", Type: entities.SubProcess, Nodes: []*entities.Node{
				{ID: "label", Type: entities.ServiceTask, ParentID: "pack"},
			}},
			// Parents that form a ring.
			{ID: "ring-a", Type: entities.SubProcess, ParentID: "ring-b"},
			{ID: "ring-b", Type: entities.SubProcess, ParentID: "ring-a"},
			{ID: "end", Type: entities.EndEvent},
		},
	}

	for _, tc := range []struct{ step, want string }{
		{"notify", "research"},
		{"fetch", "research"},
		{"inner-notify", "inner-research"},
		{"inner-research", "research"},
		{"score", "review"},
		{"label", ""},
		{"research", ""},
		{"start", ""},
		{"ring-a", ""},
	} {
		t.Run(tc.step, func(t *testing.T) {
			got := ""
			if adHoc := enclosingAdHoc(def, def.FindNode(tc.step)); adHoc != nil {
				got = adHoc.ID
			}
			if got != tc.want {
				t.Fatalf("enclosingAdHoc(%s) = %q, want %q", tc.step, got, tc.want)
			}
		})
	}

	if enclosingAdHoc(def, nil) != nil || enclosingAdHoc(nil, def.Nodes[0]) != nil {
		t.Error("no step, or no definition, is inside nothing")
	}
}

// A step is withdrawn with its ad-hoc sub-process when it no longer holds the
// job's token and the sub-process holds none either. A step that lost its
// token while the sub-process is still open was not withdrawn by it.
func TestAStepIsWithdrawnWithItsAdHocSubProcessOnlyWhenTheSubProcessHasFinished(t *testing.T) {
	research := &entities.Node{ID: "research", Type: entities.SubProcess, IsAdHoc: true}
	notify := &entities.Node{ID: "notify", Type: entities.ServiceTask, ParentID: "research"}
	state := func(status entities.ProcessStatus, tokens ...*entities.Node) *entities.ProcessInstance {
		instance := &entities.ProcessInstance{Status: status}
		for _, node := range tokens {
			instance.AddToken(node)
		}
		return instance
	}

	for _, tc := range []struct {
		name     string
		instance *entities.ProcessInstance
		want     bool
	}{
		{"the step is waiting", state(entities.ProcessActive, research, notify), false},
		{"the step lost its token, the sub-process is open", state(entities.ProcessActive, research), false},
		{"the sub-process finished and took the step", state(entities.ProcessActive), true},
		{"the instance has ended", state(entities.ProcessCompleted), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := withdrawnWithAdHoc(tc.instance, research, notify, ""); got != tc.want {
				t.Fatalf("withdrawnWithAdHoc = %v, want %v", got, tc.want)
			}
		})
	}
}
