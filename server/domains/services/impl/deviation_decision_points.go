package impl

import (
	"cmp"
	"slices"
	"strings"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic"
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

// decisionPointsReading lists every place in a definition that decides from a
// value the step nodeID would have set, sorted by node id and then kind.
//
// It is what stops a waive from leaving a gateway to route on a value nobody
// supplied (AGENTS.md §0: no silent default at a decision point).
// takenFromStep is the fields the step's form declares — the values the
// process expects from it — and outputs is what the waiver supplies. Each
// point says which of those fields it reads, which the waiver supplies and
// which it does not; a value the instance already holds does not count, since
// on a second visit it is the previous visit's answer.
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
// whole, and neither is asked again for this visit.
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
// A definition is somebody's input. This is one pass over its nodes and flows
// with no recursion, each node read once however the definition nests or
// loops.
func decisionPointsReading(def *entities.ProcessDefinition, nodeID string, takenFromStep map[string]struct{},
	outputs map[string]any, reads decisionReads) []entities.DecisionPoint {
	if def == nil {
		return nil
	}
	nodes, outgoing := definitionParts(def)

	// Two nodes of one id are one point: the engine runs whichever it finds,
	// so the point reads what either does.
	var found []entities.DecisionPoint
	at := map[[2]string]int{}
	for _, node := range nodes {
		for _, point := range decisionPointsAt(node, outgoing[node.ID], nodeID, takenFromStep, reads) {
			key := [2]string{point.NodeID, string(point.Kind)}
			if i, listed := at[key]; listed {
				found[i].Reads = unionOfNames(found[i].Reads, point.Reads)
				found[i].Analysed = found[i].Analysed && point.Analysed
				found[i].HasDefaultFlow = found[i].HasDefaultFlow && point.HasDefaultFlow
				continue
			}
			at[key] = len(found)
			found = append(found, point)
		}
	}
	return relevantTo(found, takenFromStep, outputs)
}

// definitionParts is every node of a definition, whatever it is nested in, in
// the order the definition lists them, and every flow by the node it leaves.
//
// A queue and a record of what has been taken, not a recursion: a definition
// nested ten thousand deep costs ten thousand entries, and a node listed twice
// — or inside itself — is taken once.
func definitionParts(def *entities.ProcessDefinition) ([]*entities.Node, map[string][]*entities.SequenceFlow) {
	outgoing := map[string][]*entities.SequenceFlow{}
	addFlows := func(flows []*entities.SequenceFlow) {
		for _, flow := range flows {
			if flow != nil {
				outgoing[flow.SourceRef] = append(outgoing[flow.SourceRef], flow)
			}
		}
	}
	addFlows(def.Flows)

	taken := map[*entities.Node]struct{}{}
	nodes := make([]*entities.Node, 0, len(def.Nodes))
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
		nodes = append(nodes, node)
		addFlows(node.Flows)
		pending = append(pending, node.Nodes...)
	}
	return nodes, outgoing
}

// decisionPointsAt is the decision points one node is, with everything each
// reads. A node can be more than one: a repeating step reads its list and its
// completion condition. waived is the step being waived, whose own repeating
// is not asked again.
func decisionPointsAt(node *entities.Node, flows []*entities.SequenceFlow, waived string,
	declared map[string]struct{}, reads decisionReads) []entities.DecisionPoint {
	var points []entities.DecisionPoint
	add := func(point entities.DecisionPoint, is bool) {
		if is {
			points = append(points, point)
		}
	}
	add(gatewayPoint(node, flows, declared))
	add(conditionalEventPoint(node, declared))
	add(decisionTablePoint(node, reads))
	add(calledProcessPoint(node, declared))
	if node.ID != waived {
		add(completionConditionPoint(node, declared))
		add(collectionPoint(node))
	}
	return points
}

// decisionPointOf is a point at node of a kind, reading names.
func decisionPointOf(node *entities.Node, kind entities.DecisionPointKind, names []string, analysed bool) entities.DecisionPoint {
	return entities.DecisionPoint{
		NodeID: node.ID, NodeName: cmp.Or(node.Name, node.ID), Kind: kind,
		Reads: unionOfNames(names, nil), Analysed: analysed,
	}
}

// conditionReads is what a condition the evaluator chain is given reads:
// the names in it (logic.ReferencedNames), and its whole text when the step
// declares a field of exactly that name — the chain looks the text up as a
// variable before it parses it (SimpleVariableEvaluator), and a form field may
// be named anything.
func conditionReads(condition string, declared map[string]struct{}) (names []string, analysable bool) {
	names, analysable = logic.ReferencedNames(condition)
	if _, isField := declared[condition]; isField && condition != "" {
		names = append(names, condition)
	}
	return names, analysable
}

// gatewayPoint is an exclusive or inclusive gateway choosing among its flows:
// the only two steps whose flows' conditions the engine evaluates.
func gatewayPoint(node *entities.Node, flows []*entities.SequenceFlow, declared map[string]struct{}) (entities.DecisionPoint, bool) {
	if node.Type != entities.ExclusiveGateway && node.Type != entities.InclusiveGateway {
		return entities.DecisionPoint{}, false
	}
	var names []string
	analysed, hasDefault := true, false
	for _, flow := range flows {
		read, analysable := conditionReads(flow.Condition, declared)
		names = append(names, read...)
		analysed = analysed && analysable
		// A default the gateway names and does not have is no default: the
		// engine raises an incident.
		hasDefault = hasDefault || (node.DefaultFlow != "" && flow.ID == node.DefaultFlow)
	}
	point := decisionPointOf(node, entities.DecisionPointGateway, names, analysed)
	point.HasDefaultFlow = hasDefault
	return point, true
}

// conditionalEventPoint is a step that waits for a condition. Any step: the
// engine re-reads a condition_expression wherever a token rests on one
// (waitingConditionalNodes), not only on a catch event.
func conditionalEventPoint(node *entities.Node, declared map[string]struct{}) (entities.DecisionPoint, bool) {
	condition := node.GetStringProperty("condition_expression")
	if condition == "" {
		return entities.DecisionPoint{}, false
	}
	names, analysable := conditionReads(condition, declared)
	return decisionPointOf(node, entities.DecisionPointConditionalEvent, names, analysable), true
}

// completionConditionPoint is a repeating step or an ad-hoc sub-process
// deciding whether it is done. On any other step the condition is never read.
func completionConditionPoint(node *entities.Node, declared map[string]struct{}) (entities.DecisionPoint, bool) {
	if node.CompletionCondition == "" || (!node.Repeats() && !node.IsAdHoc) {
		return entities.DecisionPoint{}, false
	}
	names, analysable := conditionReads(node.CompletionCondition, declared)
	return decisionPointOf(node, entities.DecisionPointCompletionCondition, names, analysable), true
}

// collectionPoint is a repeating step that takes its list from a variable,
// which it looks up by exactly that name. Without the list it runs once, with
// no error (NodeHandlerTemplate.handleMultiInstance).
func collectionPoint(node *entities.Node) (entities.DecisionPoint, bool) {
	if !node.Repeats() || node.Collection == "" {
		return entities.DecisionPoint{}, false
	}
	return decisionPointOf(node, entities.DecisionPointCollection, []string{node.Collection}, true), true
}

// decisionTablePoint is a step that consults a decision table, read as the
// engine evaluates it.
//
// A business rule task with an input mapping evaluates its table with the
// mapping's targets, so what the table reads of the process is the mapping's
// sources, whatever the table's columns are called. Otherwise the table sees
// every variable, and reads what the lookup says the consulted version reads.
// A decision nobody can find, or one that could not be read, is not analysed.
func decisionTablePoint(node *entities.Node, reads decisionReads) (entities.DecisionPoint, bool) {
	key, version, mapping := decisionConsulted(node)
	if key == "" {
		return entities.DecisionPoint{}, false
	}
	if len(mapping) > 0 {
		names, analysable := logic.MappingSourceNames(mapping)
		return decisionPointOf(node, entities.DecisionPointDecisionTable, names, analysable), true
	}
	if reads == nil {
		return decisionPointOf(node, entities.DecisionPointDecisionTable, nil, false), true
	}
	names, analysable, found := reads(key, version)
	return decisionPointOf(node, entities.DecisionPointDecisionTable, names, analysable && found), true
}

// decisionConsulted is the decision a step consults, the version it pins (0
// for the one in force) and the mapping its table is fed through, as the two
// handlers that consult one read them: BusinessRuleTaskHandler and
// UserTaskHandler.resolveAssignment. No other step consults a decision,
// whatever its properties say.
func decisionConsulted(node *entities.Node) (key string, version int, mapping map[string]any) {
	switch node.Type {
	case entities.BusinessRuleTask:
		if fed, isMapping := node.Properties[inputMappingProperty].(map[string]any); isMapping {
			mapping = fed
		}
		return node.GetStringProperty("decision_key"), pinnedVersion(node.Properties["decision_version"]), mapping
	case entities.UserTask:
		return strings.TrimSpace(node.GetStringProperty(handlerAssignmentDecisionKey)),
			pinnedVersion(node.Properties[assignmentDecisionVersionProperty]), nil
	}
	return "", 0, nil
}

// pinnedVersion reads a version from a step's properties as both handlers do:
// a number is a version, anything else — text included — is none. Zero or
// less is the version in force, as the decision service reads it.
func pinnedVersion(raw any) int {
	switch v := raw.(type) {
	case float64:
		return max(int(v), 0)
	case int:
		return max(v, 0)
	}
	return 0
}

// calledProcessPoint is a call activity that hands the process it calls a
// field the step declares: every variable when it has no input mapping, and
// otherwise the variables its mapping names (CallActivityHandler looks each
// source up by exactly that name). What the called process does with them is
// not read, so the point is never analysed. A call activity handed none of
// the fields is no point at all.
func calledProcessPoint(node *entities.Node, declared map[string]struct{}) (entities.DecisionPoint, bool) {
	if node.Type != entities.CallActivity {
		return entities.DecisionPoint{}, false
	}
	var handed []string
	mapping, isMapping := node.Properties["in_mapping"].(map[string]any)
	if !isMapping || len(mapping) == 0 {
		handed = sortedKeys(declared)
	}
	for _, source := range mapping {
		name, isText := source.(string)
		if _, isField := declared[name]; isText && isField {
			handed = append(handed, name)
		}
	}
	if len(handed) == 0 {
		return entities.DecisionPoint{}, false
	}
	return decisionPointOf(node, entities.DecisionPointCalledProcess, handed, false), true
}

// relevantTo turns what each point reads into what it takes from the step:
// a point that reads nothing the step declares is dropped, unless what it
// reads could not be told, and the rest say what is supplied and what is
// missing.
func relevantTo(found []entities.DecisionPoint, takenFromStep map[string]struct{}, outputs map[string]any) []entities.DecisionPoint {
	var points []entities.DecisionPoint
	for _, point := range found {
		for _, name := range point.Reads {
			if _, declared := takenFromStep[name]; !declared {
				continue
			}
			if _, given := outputs[name]; given {
				point.Supplied = append(point.Supplied, name)
			} else {
				point.Missing = append(point.Missing, name)
			}
		}
		if point.Analysed && len(point.Supplied)+len(point.Missing) == 0 {
			continue
		}
		if point.Kind == entities.DecisionPointCalledProcess {
			// Handed over, not known to be read: nothing is known to be missing.
			point.Missing = nil
		}
		points = append(points, point)
	}
	slices.SortFunc(points, byNodeThenKind)
	return points
}

// byNodeThenKind orders decision points for a plan: by the node's id, then by
// kind, so the same definition always reads the same way.
func byNodeThenKind(a, b entities.DecisionPoint) int {
	return cmp.Or(strings.Compare(a.NodeID, b.NodeID), strings.Compare(string(a.Kind), string(b.Kind)))
}

// unionOfNames is the names in either list, sorted, each once, and nil when
// there are none.
func unionOfNames(a, b []string) []string {
	if len(a)+len(b) == 0 {
		return nil
	}
	names := slices.Concat(a, b)
	slices.Sort(names)
	return slices.Compact(names)
}
