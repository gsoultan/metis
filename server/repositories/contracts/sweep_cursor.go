package contracts

import (
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// SweepCursor is how far a sweep has read: the deadline and the id of the last
// request it was answered. The sweep's reads are ordered by deadline and then
// by id, and answer only what comes strictly after the cursor. The zero
// cursor is the start.
//
// It is what lets a sweep get past a request it could not close. Read from
// the start each time, such a request would be answered again and again —
// requests that cannot be closed only get older, and the oldest is read
// first — and nothing behind it would ever be reached.
type SweepCursor struct {
	ExpiresAt time.Time
	ID        uuid.UUID
}

// SweepCursorAt is the cursor that stands at a request: a read after it
// answers what follows that request.
func SweepCursorAt(request entities.DeviationRequest) SweepCursor {
	return SweepCursor{ExpiresAt: request.ExpiresAt, ID: request.ID}
}

// AtStart reports whether the cursor is the zero one, before every request.
func (c SweepCursor) AtStart() bool {
	return c.ID == uuid.Nil && c.ExpiresAt.IsZero()
}
