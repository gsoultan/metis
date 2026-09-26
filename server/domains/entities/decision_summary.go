package entities

import "github.com/google/uuid"

// DecisionSummary is one decision key, as its newest version, without the
// table: enough to name it, open it, and say which decisions it requires.
//
// The dependency graph and a step's decision picker need every key a project
// has. They were given full definitions to learn that — every version of
// every table, its lines and its examples included — a thousand at a time,
// before the page showed a row.
type DecisionSummary struct {
	ID                uuid.UUID `json:"id"`
	Key               string    `json:"key"`
	Name              string    `json:"name"`
	Version           int       `json:"version"`
	RequiredDecisions []string  `json:"required_decisions,omitzero"`
}
