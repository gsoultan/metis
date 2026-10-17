package entities

import (
	"time"

	"github.com/google/uuid"
)

// MaxRetentionHoldReasonLength is the longest reason a person may give for
// placing or releasing a retention hold, in characters: the same bound as a
// deviation's, for the same reason.
const MaxRetentionHoldReasonLength = MaxDeviationReasonLength

// RetentionHold keeps one instance out of the finished-instance sweep, with
// the whole case of calls it is part of — the instance that started the chain
// and every instance started from it — for as long as it stands.
//
// It is what a legal hold, an investigation or an auditor's request is
// recorded as: who asked for the case to be kept, when, and why. Releasing
// it removes it; the audit trail keeps both acts.
//
// No JSON or ORM tags: an endpoint maps it to a view, and a repository maps it
// to a row.
type RetentionHold struct {
	ID        uuid.UUID
	CreatedAt time.Time
	Project   *Project
	Instance  *ProcessInstance

	Reason     string
	PlacedBy   string
	PlacedByID uuid.UUID
}
