package contracts

import (
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// DeviationRequestChange is one move of a request from one status to another,
// with what is known at that moment.
//
// Status is where the request goes and is always given. Every other field is
// left as stored when it is zero; a nil Outcome keeps the stored one, and an
// empty one that is not nil replaces it with nothing.
//
// A decision a person makes has to say who and when. A move that would leave
// a request approved, applied or rejected without a DecidedBy, a DecidedByID
// and a DecidedAt — given by the change, or already stored — is refused: a
// request approved by no account reads as one a second person approved, and
// one approved at no time as a run that never reported. So a waiting request
// is approved, applied or rejected only by a change that gives all three; a
// run's report on an approved request (applied) gives none and keeps the
// approval's. An expiry, a stale request and an interruption name nobody: the
// clock, the instance or a failed run made them.
//
// Who approved is written once. A change that moves a request out of approved
// or interrupted is a report — the run's, or the sweep's — and says what
// happened: Status and Outcome. One that gives a DecidedBy, a DecidedByID, a
// DecidedAt or a DecisionReason is refused, whatever their values.
type DeviationRequestChange struct {
	Status         entities.DeviationRequestStatus
	DecidedBy      string
	DecidedByID    uuid.UUID
	DecisionReason string
	DecidedAt      time.Time
	Outcome        map[string]any
}
