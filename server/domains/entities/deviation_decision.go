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
	// Organization is the organization a self-approval was allowed in: the
	// request's own, which the installation names as having one
	// administrator. Set only when SelfApproved is — it is what the
	// exception rested on, and the record says it.
	Organization uuid.UUID
	// OtherAdministrators is how many other administrators of that
	// organization the lookup the approval made found — none, or the
	// approval would not be the requester's own. It is kept as what the
	// lookup answered, so that what the record says of it is a witness and
	// not a constant. Meaningful only when SelfApproved is.
	OtherAdministrators int
}
