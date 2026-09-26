package entities

import (
	"time"

	"github.com/google/uuid"
)

// DecisionSummary is one decision key, without the table: enough to name it,
// open it, say which version of it is in force, and say which decisions it
// requires.
//
// One per key rather than one per version. Every save of a decision is a new
// version, so a list of versions shows a table edited five times five times,
// and not which of the five is in force.
type DecisionSummary struct {
	// ID, Name, Version, HitPolicy and RequiredDecisions are the live
	// version's — what opening the row opens and what an evaluation that names
	// no version reads — or the newest's when no version is live.
	ID                uuid.UUID `json:"id"`
	Key               string    `json:"key"`
	Name              string    `json:"name"`
	Version           int       `json:"version"`
	HitPolicy         string    `json:"hit_policy,omitzero"`
	RequiredDecisions []string  `json:"required_decisions,omitzero"`

	// LiveVersion is the version in force, zero when none is. NewestVersion
	// is the highest stored; above LiveVersion, it is staged: saved, and
	// waiting to be made live.
	LiveVersion   int `json:"live_version"`
	NewestVersion int `json:"newest_version"`

	// LastChangedAt is when a version was last saved or made live.
	LastChangedAt time.Time `json:"last_changed_at,omitzero"`
}
