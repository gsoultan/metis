package entities

import (
	"maps"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// ProcessInstance represents a running process instance.
type ProcessInstance struct {
	ID             uuid.UUID          `json:"id"`
	Project        *Project           `json:"project,omitzero"`
	Definition     *ProcessDefinition `json:"definition,omitzero"`
	ParentInstance *ProcessInstance   `json:"parent_instance,omitzero"`
	ParentNode     *Node              `json:"parent_node,omitzero"`
	// RootInstance is the top-level process instance that originally spawned this one.
	// It equals the instance itself when there is no parent (i.e., this is the root).
	RootInstance     *ProcessInstance `json:"root_instance,omitzero"`
	Status           ProcessStatus    `json:"status"` // e.g., "active", "completed"
	Variables        map[string]any   `json:"variables,omitzero"`
	Tokens           []Token          `json:"tokens,omitzero"`
	CompletedNodes   []*Node          `json:"completed_nodes,omitzero"`
	CompensatedNodes []*Node          `json:"compensated_nodes,omitzero"`
	// MultiInstance holds engine bookkeeping for nodes that run once per item,
	// keyed by node ID. Deliberately separate from Variables.
	MultiInstance map[string]MultiInstanceState `json:"multi_instance,omitzero"`
	// Joins counts the branches that have reached each waiting gateway, keyed by
	// node ID. Separate from Variables for the same reason as MultiInstance.
	Joins     map[string]int `json:"joins,omitzero"`
	CreatedAt time.Time      `json:"created_at,omitzero"`
}

func (pi *ProcessInstance) AddToken(node *Node) Token {
	return pi.AddTokenWithIteration(node, "")
}

func (pi *ProcessInstance) AddTokenWithIteration(node *Node, iterationID string) Token {
	token := NewToken(pi, node)
	token.IterationID = iterationID
	pi.Tokens = append(pi.Tokens, token)
	return token
}

// containsNode reports whether nodes already holds the given node.
//
// Identity here is the node ID, never the pointer. The instance adapter rebuilds
// CompletedNodes and CompensatedNodes as freshly allocated *Node values on every
// load, so two pointers to the same BPMN node are never equal once an instance
// has been read back from the database — and a pointer-based check would stop
// deduping at exactly the moment it matters.
func containsNode(nodes []*Node, node *Node) bool {
	if node == nil {
		return false
	}
	return slices.ContainsFunc(nodes, func(n *Node) bool {
		return n != nil && n.ID == node.ID
	})
}

func (pi *ProcessInstance) MarkCompleted(node *Node) {
	if node == nil || containsNode(pi.CompletedNodes, node) {
		return
	}
	pi.CompletedNodes = append(pi.CompletedNodes, node)
}

func (pi *ProcessInstance) MarkCompensated(node *Node) {
	if node == nil || containsNode(pi.CompensatedNodes, node) {
		return
	}
	pi.CompensatedNodes = append(pi.CompensatedNodes, node)
}

// IsCompensated reports whether this activity has already been rolled back.
// Compensation has to be idempotent: a retried or re-thrown compensation must
// not undo the same activity a second time.
func (pi *ProcessInstance) IsCompensated(node *Node) bool {
	return containsNode(pi.CompensatedNodes, node)
}

// IsCompleted reports whether this activity has already finished on this instance.
func (pi *ProcessInstance) IsCompleted(node *Node) bool {
	return containsNode(pi.CompletedNodes, node)
}

// RemoveTokenByNode drops every token sitting on node.
//
// A nil node removes nothing. The engine reaches here with one whenever a
// token's node is absent from the definition it just loaded — the shape an
// instance is left in if it is ever pointed at a version whose graph does not
// contain the node it was waiting on. This used to dereference the nil inside
// the comparison, which did not panic and did not return: the goroutine spun
// forever, holding the transaction it was called in, with no incident and no
// error to say so. Whoever completed that task simply never got a response.
//
// Callers that know the node ID but not the node should use RemoveTokenByNodeID.
func (pi *ProcessInstance) RemoveTokenByNode(node *Node) {
	if node == nil {
		return
	}
	pi.RemoveTokenByNodeID(node.ID)
}

// RemoveTokenByNodeID drops every token sitting on nodeID.
//
// The engine knows the ID it is advancing past even when the definition no
// longer describes that node, so this is the form that can still clear the token
// in the case RemoveTokenByNode has to refuse.
func (pi *ProcessInstance) RemoveTokenByNodeID(nodeID string) {
	pi.Tokens = slices.DeleteFunc(pi.Tokens, func(t Token) bool {
		return t.Node != nil && t.Node.ID == nodeID
	})
}

// RemoveTokenByIteration drops the token for one iteration of a multi-instance
// node. Nil-safe for the same reason RemoveTokenByNode is.
func (pi *ProcessInstance) RemoveTokenByIteration(node *Node, iterationID string) {
	if node == nil {
		return
	}
	pi.Tokens = slices.DeleteFunc(pi.Tokens, func(t Token) bool {
		return t.Node != nil && t.Node.ID == node.ID && t.IterationID == iterationID
	})
}

// WaitingIteration names the iteration of node that a completion retires, and
// reports whether there is one.
//
// A completion that names its iteration gets that one, if its token is still
// on the node; if it is not, the iteration has already been counted or was
// withdrawn, and there is nothing to retire.
//
// A completion that names none gets the lowest-numbered iteration still
// waiting. That is a task created before migration 31, which recorded no
// iteration, and every caller that finishes a run without knowing which it was
// — a call activity's child ending, an external task. Lowest, in numeric
// order, because it is the same answer every time and needs nothing the old
// row lacks; for a sequential step it is the only one there is.
//
// A token with no iteration is not an iteration: it is a step running once,
// which HasPlainToken answers.
func (pi *ProcessInstance) WaitingIteration(node *Node, iterationID string) (string, bool) {
	if node == nil {
		return "", false
	}
	lowest, found := "", false
	for _, token := range pi.Tokens {
		if token.Node == nil || token.Node.ID != node.ID || token.IterationID == "" {
			continue
		}
		if iterationID != "" {
			if token.IterationID == iterationID {
				return iterationID, true
			}
			continue
		}
		if !found || iterationBefore(token.IterationID, lowest) {
			lowest, found = token.IterationID, true
		}
	}
	return lowest, found
}

// iterationBefore orders iteration ids as the engine numbers them: "2" before
// "10". An id that is not a number sorts after every number, so a number is
// always preferred, and two such ids fall back to text order.
func iterationBefore(a, b string) bool {
	left, leftErr := strconv.Atoi(a)
	right, rightErr := strconv.Atoi(b)
	switch {
	case leftErr == nil && rightErr == nil:
		return left < right
	case leftErr == nil:
		return true
	case rightErr == nil:
		return false
	default:
		return a < b
	}
}

// HasPlainToken reports whether node holds a token that belongs to no
// iteration — the step running once, as every step that does not repeat does,
// and as a repeating step given nothing to repeat over does.
func (pi *ProcessInstance) HasPlainToken(node *Node) bool {
	if node == nil {
		return false
	}
	return slices.ContainsFunc(pi.Tokens, func(t Token) bool {
		return t.Node != nil && t.Node.ID == node.ID && t.IterationID == ""
	})
}

// WaitsFor reports whether node is still waiting for the run iterationID
// names — or, when it names none, for any run at all.
//
// It is the one answer to that question. The engine asks it before it counts a
// completion, a job before it makes its call and again before it uses the
// result, a worker's report before it is accepted, a called process before it
// resumes its parent. They used to ask three different things — "is there a
// token on the step", "is the step counting", "is there a token for this run"
// — and where the answers differed something was called, or resumed, and then
// refused.
//
// A step that does not repeat is waiting while it holds a token.
//
// A step that repeats is waiting for a run while it is counting its runs and
// holds that run's token; given nothing to repeat over it runs once, on a
// token that belongs to no run, and is waiting while it holds that. Iteration
// tokens on a step that is not counting are not runs anybody is waiting for:
// they are what a release before migration 31 left behind when a step ended,
// and the step has finished.
//
// One state has no token to show. A sub-process's runs happen on the steps
// inside it, and before this rule the sub-process gave up its tokens as it was
// entered. An instance that was inside one at the upgrade is counting and
// holds nothing, and a run that finishes is one the step is waiting for. A
// sub-process entered since keeps a token per run until the run finishes, so
// counting with no token at all can only be that older state.
func (pi *ProcessInstance) WaitsFor(node *Node, iterationID string) bool {
	if node == nil {
		return false
	}
	if !node.Repeats() {
		return len(pi.GetTokensByNode(node)) > 0
	}
	if !pi.IsMultiInstanceActive(node.ID) {
		return iterationID == "" && pi.HasPlainToken(node)
	}
	if _, waiting := pi.WaitingIteration(node, iterationID); waiting {
		return true
	}
	return iterationID == "" && node.RunsInside() && len(pi.GetTokensByNode(node)) == 0
}

// GetTokensByNode returns every token sitting on node. Nil-safe: a node the
// definition does not describe holds no tokens as far as callers are concerned,
// which is the same answer the other accessors here give.
func (pi *ProcessInstance) GetTokensByNode(node *Node) []Token {
	if node == nil {
		return nil
	}
	var out []Token
	for _, t := range pi.Tokens {
		if t.Node != nil && t.Node.ID == node.ID {
			out = append(out, t)
		}
	}
	return out
}

func (pi *ProcessInstance) SetVariable(key string, value any) {
	if pi.Variables == nil {
		pi.Variables = make(map[string]any)
	}
	pi.Variables[key] = value
}

// BindMultiInstanceElement makes the current item visible to the iteration
// about to run, under the name the author chose.
//
// The item used to be stored as "_mi_var_<node>_<n>", which nothing ever read,
// so a task told to run once per supplier ran the right number of times and
// could not tell one supplier from another — and those keys were then left in
// the instance's variables for good.
//
// The value is set on the shared variables rather than passed alongside them
// because that is what a step reads. Iterations are started one at a time, and
// a task takes its own copy of the variables as it starts, so each one leaves
// with the item it was given.
func BindMultiInstanceElement(instance *ProcessInstance, node Node, collection []any, index int) {
	if instance == nil || node.ElementVariable == "" {
		return
	}
	if index < 0 || index >= len(collection) {
		return
	}
	instance.SetVariable(node.ElementVariable, collection[index])
}

// MultiInstanceCollection returns the list a node iterates over, and whether it
// has one — a node may instead be given a plain count.
func MultiInstanceCollection(instance *ProcessInstance, node Node) ([]any, bool) {
	if instance == nil || node.Collection == "" {
		return nil, false
	}
	items, ok := instance.Variables[node.Collection].([]any)
	return items, ok
}

// MultiInstanceState tracks the progress of one node that runs once per item.
//
// This used to live in the instance's variables as `_mi_<node>_active`,
// `_mi_<node>_completed` and `_mi_<node>_total`. That put engine bookkeeping in
// the same namespace as business data, where it collided with user variables and
// leaked into the UI, audit history, variable snapshots and every script and
// condition scope. Keeping it in its own field means the variables map holds
// only what the process is actually about.
type MultiInstanceState struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
}

// StartMultiInstance records that a node has begun running once per item.
func (pi *ProcessInstance) StartMultiInstance(nodeID string, total int) {
	if pi.MultiInstance == nil {
		pi.MultiInstance = map[string]MultiInstanceState{}
	}
	pi.MultiInstance[nodeID] = MultiInstanceState{Total: total}
}

// IsMultiInstanceActive reports whether the node is already running its
// iterations, so re-entering it does not start them again.
func (pi *ProcessInstance) IsMultiInstanceActive(nodeID string) bool {
	_, ok := pi.MultiInstance[nodeID]
	return ok
}

// CompleteMultiInstanceIteration counts one finished iteration and returns the
// running totals.
func (pi *ProcessInstance) CompleteMultiInstanceIteration(nodeID string) (completed, total int) {
	state, ok := pi.MultiInstance[nodeID]
	if !ok {
		return 0, 0
	}
	state.Completed++
	pi.MultiInstance[nodeID] = state
	return state.Completed, state.Total
}

// MultiInstanceProgress reports how far a node has got.
func (pi *ProcessInstance) MultiInstanceProgress(nodeID string) (completed, total int, ok bool) {
	state, found := pi.MultiInstance[nodeID]
	return state.Completed, state.Total, found
}

// FinishMultiInstance drops the bookkeeping once every iteration is done.
func (pi *ProcessInstance) FinishMultiInstance(nodeID string) {
	delete(pi.MultiInstance, nodeID)
}

// MultiInstanceConditionScope returns the variables a completion condition is
// evaluated against: the business variables plus the counters BPMN 2.0 defines
// for a multi-instance activity (§10.2.7).
//
// The returned map is a copy — the counters describe progress and are not
// process data, so they are visible to the condition without being stored on the
// instance or reaching the audit trail.
func (pi *ProcessInstance) MultiInstanceConditionScope(nodeID string) map[string]any {
	scope := make(map[string]any, len(pi.Variables)+3)
	maps.Copy(scope, pi.Variables)
	if state, ok := pi.MultiInstance[nodeID]; ok {
		scope["nrOfInstances"] = state.Total
		scope["nrOfCompletedInstances"] = state.Completed
		scope["nrOfActiveInstances"] = state.Total - state.Completed
	}
	return scope
}

// RecordJoinArrival counts one branch reaching a gateway that waits for several,
// and returns how many have now arrived.
func (pi *ProcessInstance) RecordJoinArrival(nodeID string) int {
	if pi.Joins == nil {
		pi.Joins = map[string]int{}
	}
	pi.Joins[nodeID]++
	return pi.Joins[nodeID]
}

// JoinArrivals reports how many branches have reached the gateway so far.
func (pi *ProcessInstance) JoinArrivals(nodeID string) int {
	return pi.Joins[nodeID]
}

// ClearJoin drops the count once every branch has arrived and the gateway has
// let the process through.
func (pi *ProcessInstance) ClearJoin(nodeID string) {
	delete(pi.Joins, nodeID)
}
