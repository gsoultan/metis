package impl

import (
	"cmp"
	"slices"
	"strings"

	"github.com/gsoultan/metis/server/domains/entities"
)

// decisionReads says what one version of a decision reads from the variables
// it is evaluated with — its own columns and cells and those of every decision
// it requires (logic.DecisionTableReads) — and whether that could be told.
// version 0 is the version in force; any other is that version exactly, which
// is the one a step that pins a version is evaluated with. found is false when
// there is no such decision or no such version.
type decisionReads func(key string, version int) (names []string, analysable, found bool)

// assignmentDecisionVersionProperty mirrors handlers/impl.AssignmentDecisionVersion,
// for the reason handlerAssignmentDecisionKey gives: services must not depend
// on handlers.
const assignmentDecisionVersionProperty = "assignment_decision_version"

// AssignmentDecisionVersionForTest exposes that constant so a test can hold it
// against the handler's own. If the two drift, a user task that pins a version
// of its assignment table is read here at the version in force instead.
const AssignmentDecisionVersionForTest = assignmentDecisionVersionProperty

// decisionPointsReading lists every place in a definition that decides from a
// value the step nodeID would have set, sorted by node id and then kind.
//
// It is what stops a waive from leaving a gateway to route on a value nobody
// supplied (AGENTS.md §0: no silent default at a decision point).
// takenFromStep is the fields the step's form declares — the values the
// process expects from it — and outputs is what the waiver supplies. Each
// point says which of those fields it reads, which the waiver supplies and
// which it does not, and how many names it reads in all; a value the instance
// already holds does not count, since on a second visit it is the previous
// visit's answer.
//
// Nothing is followed. A walk along the flows from the step is right only
// while it copies every way the engine moves an instance: an event
// sub-process a message starts, a boundary event on a sub-process the step is
// inside, a step started by hand in an ad-hoc sub-process and a branch running
// beside the step are all reached without a flow from it. So every node is
// read, whatever it is inside, and a point is listed wherever it is. The cost
// is being asked for a value only a point already passed would read; the
// value is one the step's own form declares, so it can always be given.
//
// What counts, each as the engine evaluates it:
//
//   - an exclusive or inclusive gateway, by the conditions of its flows;
//   - any step with a condition_expression, which the engine re-reads on every
//     advance while a token waits there;
//   - the completion condition of a repeating step or an ad-hoc sub-process;
//   - the decision table of a business rule task, or the one that assigns a
//     user task — the version the step pins, read through its input mapping;
//   - a call activity handed one of the fields: the process it calls is not
//     read, so it is listed as not analysed and nothing is said to be missing;
//   - a repeating step whose list is one of the fields: without the list it
//     runs once and says nothing.
//
// The step's own completion condition and list are left out: a waive ends it
// whole, and neither is asked again for this visit. Unless more than one node
// carries the step's id: then which of them the instance is at is not known
// here, and both are listed for each — one more value asked for, and never one
// too few.
//
// What does not: a condition on a flow leaving anything but those two
// gateways, which the engine never evaluates (followOutgoingFlows takes every
// flow); and scripts and service-task input mappings, which compute and do not
// route. A value one of those derives from the step's is not traced.
//
// A point that reads nothing the step declares is not listed, unless what it
// reads could not be told: then it is listed with Analysed false, because "not
// known" must never read as "nothing".
//
// A definition is somebody's input, and nothing refuses one that uses an id
// many times over. This is one pass over its nodes and flows with no
// recursion, and its cost is in proportion to the definition's size whatever
// is written in it: each node is read once however the definition nests or
// loops, the flows leaving an id once however many nodes carry the id, a
// decision once however many steps consult it.
//
// Nodes that share an id are one point of each kind, reading what any of them
// reads: the engine runs whichever of them it finds, and a point that might
// read a value is a point that reads it. Its name is the first's, it is
// analysed only if every one of them is, and it has a default flow only if
// every one of them has.
//
// A point names only the fields the step declares, so its lists are never
// longer than the form, and counts everything else it reads (ReadsInAll).
//
// Points that read the same thing — one decision, or everything a call
// activity hands over — share the lists that say so. They are for reading:
// nothing here writes to a list once it is in a point, and a caller that
// wants a shorter or a different list makes its own.
func decisionPointsReading(def *entities.ProcessDefinition, nodeID string, takenFromStep map[string]struct{},
	outputs map[string]any, reads decisionReads) []entities.DecisionPoint {
	if def == nil {
		return nil
	}
	ids, copies, outgoing := definitionParts(def)
	scan := decisionScan{waived: nodeID, ownRepeatLeftOut: len(copies[nodeID]) == 1,
		declared: takenFromStep, outputs: outputs, lookup: reads}

	var points []entities.DecisionPoint
	for _, id := range ids {
		points = append(points, scan.pointsAt(copies[id], outgoing[id])...)
	}
	slices.SortFunc(points, byNodeThenKind)
	return points
}

// definitionParts is a definition taken apart by id: the ids of its nodes in
// the order the definition first lists them, the nodes that carry each —
// whatever they are nested in — and the flows that leave each.
//
// A queue and a record of what has been taken, not a recursion: a definition
// nested ten thousand deep costs ten thousand entries, and a node listed twice
// — or inside itself — is taken once.
func definitionParts(def *entities.ProcessDefinition) (ids []string, copies map[string][]*entities.Node, outgoing map[string][]*entities.SequenceFlow) {
	outgoing = map[string][]*entities.SequenceFlow{}
	addFlows := func(flows []*entities.SequenceFlow) {
		for _, flow := range flows {
			if flow != nil {
				outgoing[flow.SourceRef] = append(outgoing[flow.SourceRef], flow)
			}
		}
	}
	addFlows(def.Flows)

	taken := map[*entities.Node]struct{}{}
	copies = make(map[string][]*entities.Node, len(def.Nodes))
	pending := slices.Clone(def.Nodes)
	for i := 0; i < len(pending); i++ {
		node := pending[i]
		if node == nil {
			continue
		}
		if _, done := taken[node]; done {
			continue
		}
		taken[node] = struct{}{}
		if _, known := copies[node.ID]; !known {
			ids = append(ids, node.ID)
		}
		copies[node.ID] = append(copies[node.ID], node)
		addFlows(node.Flows)
		pending = append(pending, node.Nodes...)
	}
	return ids, copies, outgoing
}

// byNodeThenKind orders decision points for a plan: by the node's id, then by
// kind, so the same definition always reads the same way.
func byNodeThenKind(a, b entities.DecisionPoint) int {
	return cmp.Or(strings.Compare(a.NodeID, b.NodeID), strings.Compare(string(a.Kind), string(b.Kind)))
}

// unionOfNames is the names of a list, sorted, each once, and nil when there
// are none. The list is its own, with no room to grow into: appending to it
// never writes over a list that shares it.
func unionOfNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	sorted := slices.Clone(names)
	slices.Sort(sorted)
	return slices.Clip(slices.Compact(sorted))
}
