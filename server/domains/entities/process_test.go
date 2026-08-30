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
