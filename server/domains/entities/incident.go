package entities

import (
	"time"

	"github.com/google/uuid"
)

type IncidentStatus string

const (
	IncidentOpen     IncidentStatus = "open"
	IncidentResolved IncidentStatus = "resolved"
)

type Incident struct {
	ID         uuid.UUID          `json:"id"`
	Job        *Job               `json:"job,omitzero"`
	Instance   *ProcessInstance   `json:"instance,omitzero"`
	Definition *ProcessDefinition `json:"definition,omitzero"`
	Node       *Node              `json:"node,omitzero"`
	Error      string             `json:"error"`
	Status     IncidentStatus     `json:"status"`
	CreatedAt  time.Time          `json:"created_at"`
	ResolvedAt *time.Time         `json:"resolved_at,omitzero"`
}
