package entities

import (
	"time"

	"github.com/google/uuid"
)

// DeviationDecision is one administrator's answer to a request: who decided,
// why, and when.
//
// No JSON or ORM tags: a repository writes it onto the request's row.
type DeviationDecision struct {
	Decider   string
	DeciderID uuid.UUID
	Reason    string
	At        time.Time
	// SelfApproved says the decider is the requester and no second
	// administrator existed to ask.
	SelfApproved bool
}
