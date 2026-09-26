package entities

import (
	"time"

	"github.com/google/uuid"
)

// DecisionVersionStatus is one stored version of a decision key, and whether it
// is the one in force.
//
// Every save of a decision is a new version and none is ever changed, so a key
// accumulates a history. What a reader needs from it is which version
// evaluations read now, and what the others were, so an older one can be made
// live again when an edit turns out to be wrong.
type DecisionVersionStatus struct {
	ID        uuid.UUID `json:"id"`
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at,omitzero"`

	// Live is true for the single version an evaluation that names no version
	// reads.
	Live bool `json:"live"`
}
