package entities

import (
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
	CreatedAt        time.Time        `json:"created_at,omitzero"`
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

func (pi *ProcessInstance) MarkCompleted(node *Node) {
	if !slices.Contains(pi.CompletedNodes, node) {
		pi.CompletedNodes = append(pi.CompletedNodes, node)
	}
}

func (pi *ProcessInstance) MarkCompensated(node *Node) {
	if !slices.Contains(pi.CompensatedNodes, node) {
		pi.CompensatedNodes = append(pi.CompensatedNodes, node)
	}
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
