package entities

import (
	"time"

	"github.com/google/uuid"
)

// Machine is one process of this installation, as it last said it was: the
// machines running a server, which share its database. Each process says so
// itself, every few seconds, for as long as it runs.
type Machine struct {
	// ID is the process's, made when it started: a machine started again is
	// a new one, and the old one goes quiet.
	ID uuid.UUID `json:"id"`
	// Name is the machine's host name.
	Name string `json:"name"`
	// Address is where its API listens, as the process was told to.
	Address string `json:"address,omitzero"`
	// Role is all, web or worker: what the process does of the work.
	Role string `json:"role"`
	// Version is the release of Metis it runs.
	Version   string    `json:"version,omitzero"`
	StartedAt time.Time `json:"started_at"`
	// LastSeenAt is when it last said it was running. One that stopped
	// saying so is shown as it was, with when; nothing is guessed.
	LastSeenAt time.Time `json:"last_seen_at"`
}
