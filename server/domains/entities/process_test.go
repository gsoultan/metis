package entities_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// MarkCompleted and MarkCompensated dedupe on node identity, and a node's
// identity is its ID — not the address of the struct holding it.
//
// This matters because the instance adapter rebuilds CompletedNodes and
// CompensatedNodes from the database as freshly allocated *Node values. Two
// pointers to the same BPMN node are never equal across a reload, so a
// pointer-based check silently stops deduping the moment an instance is read
// back — and compensation runs a second time on an activity already rolled back.
func TestMarkCompensatedDedupesByNodeID(t *testing.T) {
	instance := &entities.ProcessInstance{}

	// The same BPMN node as two separate allocations, which is exactly what
	// loading the instance twice produces.
	first := &entities.Node{ID: "book-flight"}
	second := &entities.Node{ID: "book-flight"}

	instance.MarkCompensated(first)
	instance.MarkCompensated(second)

	if len(instance.CompensatedNodes) != 1 {
		t.Errorf("expected one compensated node, got %d — the same activity would be compensated twice", len(instance.CompensatedNodes))
	}
}

func TestMarkCompletedDedupesByNodeID(t *testing.T) {
	instance := &entities.ProcessInstance{}

	first := &entities.Node{ID: "book-flight"}
	second := &entities.Node{ID: "book-flight"}

	instance.MarkCompleted(first)
	instance.MarkCompleted(second)

	if len(instance.CompletedNodes) != 1 {
		t.Errorf("expected one completed node, got %d — the activity would be compensated once per duplicate", len(instance.CompletedNodes))
	}
}

// Distinct nodes must still be recorded separately.
func TestMarkCompensatedKeepsDistinctNodes(t *testing.T) {
	instance := &entities.ProcessInstance{}

	instance.MarkCompensated(&entities.Node{ID: "book-flight"})
	instance.MarkCompensated(&entities.Node{ID: "book-hotel"})

	if len(instance.CompensatedNodes) != 2 {
		t.Errorf("expected two compensated nodes, got %d", len(instance.CompensatedNodes))
	}
}

// A nil node must not be recorded — it carries no identity to dedupe on and
// would panic anything that later reads its ID.
func TestMarkCompensatedIgnoresNil(t *testing.T) {
	instance := &entities.ProcessInstance{}

	instance.MarkCompensated(nil)
	instance.MarkCompleted(nil)

	if len(instance.CompensatedNodes) != 0 {
		t.Errorf("a nil node was recorded as compensated: %d entries", len(instance.CompensatedNodes))
	}
	if len(instance.CompletedNodes) != 0 {
		t.Errorf("a nil node was recorded as completed: %d entries", len(instance.CompletedNodes))
	}
}

// A completion retires one iteration: the one it names, or — when it names
// none, as a task created before the iteration was recorded does — the lowest
// one still waiting. Numeric, not lexical: "10" comes after "2".
func TestACompletionRetiresTheIterationItNames(t *testing.T) {
	step := &entities.Node{ID: "approve"}
	other := &entities.Node{ID: "record"}
	withTokens := func(iterations ...string) *entities.ProcessInstance {
		instance := &entities.ProcessInstance{}
		for _, iteration := range iterations {
			instance.AddTokenWithIteration(step, iteration)
		}
		instance.AddToken(other)
		return instance
	}

	for _, tc := range []struct {
		name      string
		instance  *entities.ProcessInstance
		node      *entities.Node
		named     string
		want      string
		wantFound bool
	}{
		{"the named iteration, when it is waiting", withTokens("0", "1", "2"), step, "1", "1", true},
		{"a named iteration that is not waiting", withTokens("0", "2"), step, "1", "", false},
		{"no name takes the lowest", withTokens("2", "0", "1"), step, "", "0", true},
		{"lowest is numeric, not lexical", withTokens("10", "2"), step, "", "2", true},
		{"a number comes before a name", withTokens("b", "3"), step, "", "3", true},
		{"no name and nothing waiting", withTokens(), step, "", "", false},
		{"a plain token is not an iteration", withTokens(""), step, "", "", false},
		{"another step's tokens are not this step's", withTokens("0"), other, "", "", false},
		{"no step at all", withTokens("0"), nil, "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, found := tc.instance.WaitingIteration(tc.node, tc.named)
			if got != tc.want || found != tc.wantFound {
				t.Fatalf("WaitingIteration(%q) = %q, %v; want %q, %v", tc.named, got, found, tc.want, tc.wantFound)
			}
		})
	}

	if !withTokens("").HasPlainToken(step) {
		t.Error("a token with no iteration on the step was not found")
	}
	if withTokens("0").HasPlainToken(step) {
		t.Error("an iteration's token was mistaken for a plain one")
	}
	if withTokens("").HasPlainToken(nil) {
		t.Error("no step holds no token")
	}
}

// "Is this step still waiting for this run?" has one answer, whoever asks: the
// engine deciding whether to count a completion, a job deciding whether to
// make its call, a called process deciding whether to resume its parent.
//
// They used to answer it three ways. A token on the step read as "waiting" to
// two of them and the count decided for the third, so a step that had ended
// early before migration 31 — count gone, iteration tokens left behind —
// was called, resumed and reported to, and then refused, for ever.
func TestAStepWaitsForARunOnlyWhileItIsCountingItOrRunningOnce(t *testing.T) {
	approval := &entities.Node{ID: "approve", Type: entities.UserTask, MultiInstanceType: "parallel"}
	once := &entities.Node{ID: "record", Type: entities.UserTask}
	container := &entities.Node{ID: "sub", Type: entities.SubProcess, MultiInstanceType: "parallel"}
	adHoc := &entities.Node{ID: "research", Type: entities.SubProcess, MultiInstanceType: "parallel", IsAdHoc: true}

	holding := func(node *entities.Node, counting bool, iterations ...string) *entities.ProcessInstance {
		instance := &entities.ProcessInstance{}
		if counting {
			instance.StartMultiInstance(node.ID, 3)
		}
		for _, iteration := range iterations {
			instance.AddTokenWithIteration(node, iteration)
		}
		return instance
	}

	for _, tc := range []struct {
		name     string
		instance *entities.ProcessInstance
		node     *entities.Node
		run      string
		want     bool
	}{
		{"counting, and the run named holds its token", holding(approval, true, "0", "1"), approval, "1", true},
		{"counting, and the run named has been retired", holding(approval, true, "0"), approval, "1", false},
		{"counting, no run named, one still waiting", holding(approval, true, "2"), approval, "", true},
		{"counting, no run named, none waiting", holding(approval, true), approval, "", false},
		{"counting, with only a plain token", holding(approval, true, ""), approval, "", false},

		{"ended before migration 31: tokens left, count gone", holding(approval, false, "0", "1", "2"), approval, "", false},
		{"ended before migration 31, naming a run", holding(approval, false, "0", "1", "2"), approval, "2", false},

		{"given nothing to repeat over: runs once on a plain token", holding(approval, false, ""), approval, "", true},
		{"running once, but a run is named", holding(approval, false, ""), approval, "0", false},
		{"finished: no count and no token", holding(approval, false), approval, "", false},

		{"a step that does not repeat, holding a token", holding(once, false, ""), once, "", true},
		{"a step that does not repeat, holding none", holding(once, false), once, "", false},
		{"no step at all", holding(once, false, ""), nil, "", false},

		{"a sub-process counting, entered before it kept its tokens", holding(container, true), container, "", true},
		{"a sub-process counting and holding its runs' tokens", holding(container, true, "1", "2"), container, "", true},
		{"a sub-process that finished", holding(container, false), container, "", false},
		{"an ad-hoc sub-process keeps its token, so none means none", holding(adHoc, true), adHoc, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.instance.WaitsFor(tc.node, tc.run); got != tc.want {
				t.Fatalf("WaitsFor(%q) = %v, want %v", tc.run, got, tc.want)
			}
		})
	}
}
