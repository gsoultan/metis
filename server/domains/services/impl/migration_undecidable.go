package impl

import (
	"fmt"
	"slices"
	"strings"

	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// Where an instance can be waiting.
//
// A skip, a cancel or a hold is taken on the instances holding a token on the
// node it names. One naming a node the engine never leaves a token on was
// accepted, found no instance to act on and recorded nothing — and the plan,
// which lets work on a decided node go without anywhere to land, moved the
// instance as though the decision had been made. A hold on a boundary event
// left its waiting message on a node the new version lacks; a cancel on a
// sub-process ended nobody.
//
// So the question is asked of the engine, one node type at a time: when a
// token arrives at this node, does its handler return with the token still
// there? (server/domains/handlers/impl; the engine puts the token on a flow's
// target and then runs that node's handler.)
//
//	waits      userTask, manualTask     a task is created; the token stays
//	waits      serviceTask              a job or an external task is queued
//	waits      intermediateCatchEvent,  a subscription, a timer or a condition
//	           timerEvent
//	waits      callActivity             the parent's token stays while the
//	                                    process it called runs
//	waits      subProcess, ad-hoc       the token stays until the completion
//	                                    condition is met
//	waits      parallelGateway,         only one that joins: a branch that
//	           inclusiveGateway         arrives early keeps its token there
//	not shown  escalationThrowEvent     its handler does not advance it; left
//	                                    decidable
//
//	never      boundaryEvent            executed with no token put on it; its
//	                                    token is on the step it is attached to
//	never      subProcess, embedded     the token is taken off and put on the
//	           and event sub-process    steps inside
//	never      startEvent               passed through in the same transaction
//	never      endEvent, terminate-,    the token is removed
//	           errorEndEvent
//	never      exclusiveGateway,        the token is taken off and put on what
//	           eventBasedGateway, and   follows
//	           a gateway that only
//	           splits
//	never      scriptTask,              run and advanced at once; a failure
//	           businessRuleTask         rolls back the advance that reached it
//	never      intermediateThrowEvent,  thrown and advanced at once
//	           signalEvent, messageEvent,
//	           compensationThrowEvent
//	never      pool, lane               not steps
//
// A type that is not in this table is left decidable: refusing what could be
// taken is the worse mistake.

// passedThrough is what a refusal calls each kind of node a token never rests
// on, apart from the two that need more said (a boundary event and a
// sub-process).
var passedThrough = map[models.NodeType]string{
	models.StartEvent:             "a start event",
	models.EndEvent:               "an end event",
	models.TerminateEndEvent:      "an end event",
	models.ErrorEndEvent:          "an end event",
	models.ExclusiveGateway:       "a gateway that only routes",
	models.EventBasedGateway:      "a gateway that only hands over to the events after it",
	models.ScriptTask:             "a script task, which runs at once",
	models.BusinessRuleTask:       "a business rule task, which is decided at once",
	models.IntermediateThrowEvent: "an event the process throws",
	models.SignalEvent:            "a signal the process throws",
	models.MessageEvent:           "a message the process throws",
	models.CompensationThrowEvent: "a compensation the process throws",
}

// nobodyWaitsAt says why a decision on a node would be made about nobody, and
// nothing when an instance can be waiting there.
//
// One case is left as it was: a boundary event named together with the step it
// is attached to. A deadline on an approval the new version drops is dropped
// with it, and naming the deadline beside the approval is the only way the
// plan is told that its timer needs nowhere to land. Nothing is taken on the
// event there either, and nothing needs to be: what waits on it ends with its
// step — a skip of the step lets go of the event's waiting message and leaves
// its timer to be dismissed when due, a cancel ends the instance, a hold
// leaves it on the version it runs. The rewrite still asks, under its lock,
// that nothing is left waiting on the event (whyNotMoved).
func nobodyWaitsAt(
	node models.FlowNode,
	sourceNodes map[string]models.FlowNode,
	actions map[string]servicecontracts.NodeAction,
	incoming map[string]int,
) string {
	const aboutNobody = "so the decision would be made about nobody"
	switch node.Type {
	case models.BoundaryEvent:
		if _, withItsStep := actions[node.AttachedToRef]; withItsStep {
			return ""
		}
		host := "the step it is attached to"
		if attached, ok := sourceNodes[node.AttachedToRef]; ok {
			host = fmt.Sprintf("%q", flowNodeName(attached))
		}
		return fmt.Sprintf("it is a boundary event, and no instance ever waits at a boundary event, only at the step "+
			"it is attached to, so on its own the decision would be made about nobody; name the event together with %s, "+
			"deciding both, and what waits on the event ends with that step, or map the event to a boundary event the "+
			"new version has", host)
	case models.SubProcess:
		if node.IsAdHoc {
			return ""
		}
		return fmt.Sprintf("it is a sub-process, and an instance inside one waits at the steps inside it, never at the "+
			"sub-process itself, %s; decide the steps inside it instead%s", aboutNobody, andWhichSteps(stepsInside(node, sourceNodes, incoming)))
	case models.ParallelGateway, models.InclusiveGateway:
		if incoming[node.ID] > 1 {
			return ""
		}
		return fmt.Sprintf("it is a gateway that only splits the flow, and an instance passes straight through one, %s; "+
			"decide the step it waits at instead", aboutNobody)
	case models.Pool, models.Lane:
		return fmt.Sprintf("it is a %s, not a step, and no instance waits at one, %s", node.Type, aboutNobody)
	}
	if kind, ok := passedThrough[node.Type]; ok {
		return fmt.Sprintf("it is %s, and an instance passes straight through one without waiting there, %s; "+
			"decide the step it waits at instead", kind, aboutNobody)
	}
	return ""
}

// stepsInside is the steps of a sub-process an instance can wait at, at any
// depth, as people know them and in that order. A definition may nest a
// sub-process's nodes inside it or keep them beside it naming their parent;
// both are read, as the engine reads both.
func stepsInside(sub models.FlowNode, sourceNodes map[string]models.FlowNode, incoming map[string]int) []string {
	seen := map[string]struct{}{}
	var names []string
	var walk func(parentID string, nested []models.FlowNode)
	walk = func(parentID string, nested []models.FlowNode) {
		children := slices.Clone(nested)
		for _, id := range sortedKeys(sourceNodes) {
			if sourceNodes[id].ParentID == parentID {
				children = append(children, sourceNodes[id])
			}
		}
		for _, child := range children {
			if _, done := seen[child.ID]; done {
				continue
			}
			seen[child.ID] = struct{}{}
			if nobodyWaitsAt(child, sourceNodes, nil, incoming) == "" {
				names = append(names, fmt.Sprintf("%q", flowNodeName(child)))
			}
			walk(child.ID, child.Nodes)
		}
	}
	walk(sub.ID, sub.Nodes)
	slices.Sort(names)
	return names
}

// andWhichSteps ends a sentence with the steps to decide, or with nothing when
// there are none to name.
func andWhichSteps(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return ": " + strings.Join(names, ", ")
}

// flowNodeName is what a refusal calls a step: its name, and its id when it
// has none.
func flowNodeName(node models.FlowNode) string {
	if node.Name != "" {
		return node.Name
	}
	return node.ID
}
