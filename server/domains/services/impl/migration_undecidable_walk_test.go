package impl

import (
	"fmt"
	"strings"
	"testing"
	"time"

	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// The refusal of a decision on a sub-process names the steps inside it, and a
// process definition is somebody's input: it can nest sub-processes as deep as
// its author likes, and nothing stops a node from naming itself, or a node
// that names it, as its parent. Finding the steps inside must end whatever
// the definition says, and cost no more than one look at each node.

// refusedWithin asks for the refusals of a hold on one node and fails the test
// if the answer has not come by the deadline. It never waits for a walk that
// does not end: the test is over, and reported, at the deadline.
func refusedWithin(t *testing.T, limit time.Duration, source map[string]models.FlowNode, nodeID string) []string {
	t.Helper()
	answered := make(chan []string, 1)
	go func() {
		actions := map[string]servicecontracts.NodeAction{
			nodeID: {Kind: servicecontracts.NodeActionHold, Reason: "the checks are under review"},
		}
		answered <- (&migrationService{}).actionRefusals(source, nil, nil, actions, nil)
	}()
	select {
	case refusals := <-answered:
		return refusals
	case <-time.After(limit):
		t.Fatalf("the refusal for %q had not been worked out after %s", nodeID, limit)
		return nil
	}
}

// subProcess is an embedded sub-process beside its parent, as a definition
// that keeps its nodes flat stores it.
func subProcess(id, parentID string) models.FlowNode {
	return models.FlowNode{ID: id, Name: "Sub-process " + id, Type: models.SubProcess, ParentID: parentID}
}

// A sub-process that names itself as its parent deploys: nothing reads the
// parent when a definition is saved. The walk used to go round it for ever.
func TestTheStepsInsideASubProcessThatIsItsOwnParentAreFound(t *testing.T) {
	t.Parallel()
	source := map[string]models.FlowNode{
		"loop":  subProcess("loop", "loop"),
		"check": {ID: "check", Name: "Check the request", Type: models.UserTask, ParentID: "loop"},
	}
	refusals := refusedWithin(t, 2*time.Second, source, "loop")
	if len(refusals) != 1 || !strings.HasSuffix(refusals[0], `decide the steps inside it instead: "Check the request"`) {
		t.Fatalf("want the one refusal naming the step inside, got %v", refusals)
	}
}

// Nor does a cycle of parents, of any length, keep it going.
func TestTheStepsInsideSubProcessesThatAreEachOthersParentAreFound(t *testing.T) {
	t.Parallel()
	for _, length := range []int{2, 3, 7} {
		t.Run(fmt.Sprintf("a cycle of %d", length), func(t *testing.T) {
			source := map[string]models.FlowNode{}
			for n := range length {
				id, parent := fmt.Sprintf("s%d", n), fmt.Sprintf("s%d", (n+1)%length)
				source[id] = subProcess(id, parent)
			}
			source["check"] = models.FlowNode{ID: "check", Name: "Check the request", Type: models.UserTask, ParentID: "s0"}
			refusals := refusedWithin(t, 2*time.Second, source, "s1")
			if len(refusals) != 1 || !strings.HasSuffix(refusals[0], `decide the steps inside it instead: "Check the request"`) {
				t.Fatalf("want the one refusal naming the step inside the cycle, got %v", refusals)
			}
		})
	}
}

// nestedChain is sub-processes nested depth deep, with one task in the
// innermost, stored the two ways a definition can be: each sub-process inside
// the one before it, or all of them side by side naming their parents.
func nestedChain(depth int, flat bool) map[string]models.FlowNode {
	task := models.FlowNode{ID: "check", Name: "Check the request", Type: models.UserTask, ParentID: fmt.Sprintf("s%d", depth-1)}
	if flat {
		source := map[string]models.FlowNode{"check": task}
		for n := range depth {
			parent := ""
			if n > 0 {
				parent = fmt.Sprintf("s%d", n-1)
			}
			source[fmt.Sprintf("s%d", n)] = subProcess(fmt.Sprintf("s%d", n), parent)
		}
		return source
	}
	inner := []models.FlowNode{task}
	for n := depth - 1; n >= 0; n-- {
		sub := subProcess(fmt.Sprintf("s%d", n), "")
		sub.Nodes = inner
		inner = []models.FlowNode{sub}
	}
	return nodeIndex(inner)
}

// Each sub-process on the way down used to start the walk again from itself,
// so the work doubled with every level: a quarter of a second at fourteen
// levels, twenty-two seconds at twenty. The engine bounds how deep an instance
// may run (two hundred steps); nothing bounds how deep a definition may nest.
func TestTheStepsInsideADeeplyNestedSubProcessAreFoundAtOnce(t *testing.T) {
	t.Parallel()
	for _, flat := range []bool{false, true} {
		for _, depth := range []int{24, 200, 5000} {
			t.Run(fmt.Sprintf("flat=%v depth=%d", flat, depth), func(t *testing.T) {
				refusals := refusedWithin(t, time.Second, nestedChain(depth, flat), "s0")
				if len(refusals) != 1 || !strings.HasSuffix(refusals[0], `decide the steps inside it instead: "Check the request"`) {
					t.Fatalf("want the one refusal naming the step at the bottom, got %v", refusals)
				}
			})
		}
	}
}

// And a wide one: every node is looked at once, not once for every level.
func TestTheStepsInsideAWideSubProcessAreFoundAtOnce(t *testing.T) {
	t.Parallel()
	const tasks = 20000
	source := map[string]models.FlowNode{"wide": subProcess("wide", "")}
	for n := range tasks {
		id := fmt.Sprintf("task%05d", n)
		source[id] = models.FlowNode{ID: id, Name: id, Type: models.UserTask, ParentID: "wide"}
	}
	refusals := refusedWithin(t, 2*time.Second, source, "wide")
	if len(refusals) != 1 {
		t.Fatalf("want the one refusal, got %d", len(refusals))
	}
	// And it names the first few and counts the rest: a refusal is read by a
	// person, and must not be as long as the definition is wide.
	if !strings.HasSuffix(refusals[0], `"task00008", "task00009", and 19990 more`) || len(refusals[0]) > 600 {
		t.Fatalf("want the first ten steps named and the rest counted, got %d characters ending %q",
			len(refusals[0]), refusals[0][max(0, len(refusals[0])-60):])
	}
}
