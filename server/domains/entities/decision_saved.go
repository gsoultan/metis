package entities

import "github.com/google/uuid"

// SavedDecision is what saving a decision table did.
//
// A save never changes a stored version: an instance that evaluated version 3
// was decided under what version 3 said, and its timeline names version 3. So
// an edit becomes the next version of the key, and the caller has to be told
// which one that is — the id it was editing no longer names what it now holds.
type SavedDecision struct {
	// ID and Version name the version the save resulted in.
	ID      uuid.UUID `json:"id"`
	Version int       `json:"version"`

	// NewVersion is false when the table was the same as the version it was
	// saved from, which then stays the answer: an unchanged table is not a
	// new policy, and a copy of it would only make the history harder to read.
	NewVersion bool `json:"new_version"`

	// Live says whether that version is the one an evaluation naming no
	// version reads. A staged save leaves it false and the previous version in
	// force — unless nothing was live, when the only version is the live one.
	Live bool `json:"live"`
}
