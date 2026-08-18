package entities

import (
	"time"

	"github.com/google/uuid"
)

// TokenStatus represents the current state of a token.
type TokenStatus string

const (
	TokenActive    TokenStatus = "active"
	TokenSuspended TokenStatus = "suspended"
	TokenCompleted TokenStatus = "completed"
)

// Token represents a single point of execution in a process instance.
type Token struct {
	ID          uuid.UUID        `json:"id"`
	Instance    *ProcessInstance `json:"instance,omitzero"`
	Node        *Node            `json:"node,omitzero"`
	Status      TokenStatus      `json:"status"`
	IterationID string           `json:"iteration_id,omitzero"`
	Variables   map[string]any   `json:"variables,omitzero"`
	CreatedAt   time.Time        `json:"created_at,omitzero"`
}

func NewToken(instance *ProcessInstance, node *Node) Token {
	return Token{
		ID:        mustID(),
		Instance:  instance,
		Node:      node,
		Status:    TokenActive,
		CreatedAt: time.Now(),
	}
}
