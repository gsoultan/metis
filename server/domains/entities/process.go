package entities

import (
	"maps"
	"slices"
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
	CreatedAt     time.Time                     `json:"created_at,omitzero"`
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

func (pi *ProcessInstance) RemoveTokenByNode(node *Node) {
	pi.Tokens = slices.DeleteFunc(pi.Tokens, func(t Token) bool {
		return t.Node != nil && t.Node.ID == node.ID
	})
}

func (pi *ProcessInstance) RemoveTokenByIteration(node *Node, iterationID string) {
	pi.Tokens = slices.DeleteFunc(pi.Tokens, func(t Token) bool {
		return t.Node != nil && t.Node.ID == node.ID && t.IterationID == iterationID
	})
}

func (pi *ProcessInstance) GetTokensByNode(node *Node) []Token {
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
