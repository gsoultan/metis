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
