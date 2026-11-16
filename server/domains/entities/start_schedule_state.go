package entities

import (
	"time"

	"github.com/google/uuid"
)

// StartScheduleState is where one process's scheduled starts have got to.
//
// One per process key and project, not per version: the schedule that counts
// is the one the live version declares, and which version is live changes
// without this changing hands.
type StartScheduleState struct {
	ID         uuid.UUID
	ProjectID  uuid.UUID
	ProcessKey string
	// CheckedUntil is the watermark: every scheduled time up to it has been
	// dealt with, started or deliberately passed over. A run is owed only for
	// a time after it.
	CheckedUntil time.Time
	// NextLookAt is when the process next needs looking at: its next scheduled
	// time, or the next moment another version takes over. Nil when it has
	// nothing coming.
	NextLookAt *time.Time
	// Failures counts the looks in a row that could not be finished, and
	// LastError says why the latest could not.
	Failures  int
	LastError string
}
