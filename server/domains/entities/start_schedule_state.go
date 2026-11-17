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
	// PausedAt is when the schedule was paused, nil while it runs.
	PausedAt *time.Time
}

// StartScheduleStatus is what a person is told about a process's scheduled
// starts: whether it has any, whether they are paused, and when the next run
// is due.
type StartScheduleStatus struct {
	ProcessKey string `json:"process_key"`
	// Scheduled says a version that is live, or arranged to be, starts on a
	// schedule. False for a process that only ever starts another way.
	Scheduled bool `json:"scheduled"`
	// Paused says nothing starts on the schedule until somebody resumes it,
	// and PausedAt since when.
	Paused   bool       `json:"paused"`
	PausedAt *time.Time `json:"paused_at,omitempty"`
	// NextRunAt is when the next run starts, in UTC. Absent while paused and
	// when nothing is scheduled to come.
	NextRunAt *time.Time `json:"next_run_at,omitempty"`
	// Failures counts the attempts in a row that could not start a run, and
	// LastError says why the latest could not. The run is still owed and is
	// tried again.
	Failures  int    `json:"failures"`
	LastError string `json:"last_error,omitempty"`
}
