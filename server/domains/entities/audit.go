package entities

import (
	"time"

	"github.com/google/uuid"
)

// AuditEntry records a specific event in the lifecycle of a process instance.
type AuditEntry struct {
	ID        uuid.UUID        `json:"id"`
	Project   *Project         `json:"project,omitzero"`
	Instance  *ProcessInstance `json:"instance,omitzero"`
	Type      string           `json:"type"` // e.g., "process_started", "node_reached", "variable_updated"
	Node      *Node            `json:"node,omitzero"`
	Message   string           `json:"message"`
	Narrative string           `json:"narrative,omitzero"`
	Data      map[string]any   `json:"data,omitzero"`
	// Actor is the person whose act this entry records, when it is not the
	// one Data names under "actor": a request's entries are said by whoever
	// asked, approved or rejected it. Empty, an entry's actor is Data's
	// "actor", and an entry with neither is the engine's own.
	Actor     string    `json:"actor,omitzero"`
	Timestamp time.Time `json:"timestamp"`
}
