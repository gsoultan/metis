package bpmn_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A definition that fans out further at every step.
//
// The depth bound limits the longest path an execution follows, but every
// outgoing flow is followed, so the number of nodes run grows with the width
// of the tree rather than its depth. Thirty layers of two-way splits is thirty
// deep and 2^30 nodes, all run one after another on one worker inside one
// transaction. The budget is lowered here so the test needs a small tree; the
// shape is the same at any size.
func fanOutTree(projectID uuid.UUID, layers int) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "fan-out",
		Name:    "Fan out",
		Nodes:   []*entities.Node{{ID: "start", Type: entities.StartEvent}},
	}

	// Node "g" is the root split; "g0" and "g1" its children, and so on. The
	// last layer ends.
	frontier := []string{"g"}
	def.Nodes = append(def.Nodes, &entities.Node{ID: "g", Type: entities.ParallelGateway})
	def.Flows = append(def.Flows, &entities.SequenceFlow{ID: "f-start", SourceRef: "start", TargetRef: "g"})
	for layer := range layers {
		var next []string
		for _, parent := range frontier {
			for _, side := range []string{"0", "1"} {
				child := parent + side
				nodeType := entities.ParallelGateway
				if layer == layers-1 {
					nodeType = entities.EndEvent
				}
				def.Nodes = append(def.Nodes, &entities.Node{ID: child, Type: nodeType})
				def.Flows = append(def.Flows, &entities.SequenceFlow{
					ID: fmt.Sprintf("f-%s", child), SourceRef: parent, TargetRef: child,
				})
				next = append(next, child)
			}
		}
		frontier = next
	}
	return def
}

func TestAnExecutionThatFansOutPastItsNodeBudgetIsStopped(t *testing.T) {
	t.Setenv("METIS_MAX_EXECUTION_NODES", "100")
	h := newEngineHarness(t, "Fan Out Project")

	// Seven layers: eight deep, far inside the depth bound, and 256 nodes.
	def := fanOutTree(h.projID, 7)
	if _, err := h.svc.CreateDefinition(h.Ctx(), def); err != nil {
		t.Fatalf("deploy: %v", err)
	}

	_, err := h.svc.StartProcess(h.Ctx(), h.projID, def.Key, nil)
	if err == nil {
		t.Fatal("an execution that ran 256 nodes against a budget of 100 completed")
	}
	if !strings.Contains(err.Error(), "more than 100 nodes") || !strings.Contains(err.Error(), "METIS_MAX_EXECUTION_NODES") {
		t.Fatalf("the failure does not say the node budget was exceeded or how to raise it: %v", err)
	}
	if got := len(h.instances(t)); got != 0 {
		t.Fatalf("a start that failed left %d instance(s) behind", got)
	}
}

func TestAnExecutionInsideItsNodeBudgetCompletes(t *testing.T) {
	t.Setenv("METIS_MAX_EXECUTION_NODES", "100")
	h := newEngineHarness(t, "Fan Out Within Budget Project")

	// Five layers: 64 nodes, inside the budget.
	def := fanOutTree(h.projID, 5)
	if _, err := h.svc.CreateDefinition(h.Ctx(), def); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if _, err := h.svc.StartProcess(h.Ctx(), h.projID, def.Key, nil); err != nil {
		t.Fatalf("an execution of 64 nodes against a budget of 100 failed: %v", err)
	}
}
