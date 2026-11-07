package entities

import (
	"time"

	"github.com/google/uuid"
)

// Why nobody can act on a task. A task names who may do it by username and
// group name, never by reference, so the names outlive the people and groups
// they meant; each kind is one way that happens.
const (
	// StrandedAssigneeGone: the task is held by a name that is neither an
	// account in the organization nor a person of the project.
	StrandedAssigneeGone = "assignee_gone"
	// StrandedAssigneeInactive: the task is held by a person who was
	// deactivated.
	StrandedAssigneeInactive = "assignee_inactive"
	// StrandedNoCandidate: the task is offered to people and groups and none
	// of them has anybody left who can take it.
	StrandedNoCandidate = "no_candidate"
)

// StrandedTask is an open task nobody can act on: who it is held by or
// offered to has left, been deactivated, or been deleted, so it would wait
// for ever. What fixes it is a hand-over, and the report exists so somebody
// knows to make one.
type StrandedTask struct {
	TaskID     uuid.UUID `json:"task_id"`
	Name       string    `json:"name"`
	NodeID     string    `json:"node_id,omitzero"`
	InstanceID uuid.UUID `json:"instance_id,omitzero"`
	// Since is when the task appeared.
	Since time.Time `json:"since,omitzero"`

	// Kind is one of the Stranded* constants.
	Kind string `json:"kind"`
	// Assignee is who holds it, when somebody does.
	Assignee string `json:"assignee,omitzero"`
	// Missing names each person and group the task names and why it cannot
	// act: what a person deciding the hand-over needs to read.
	Missing []StrandedName `json:"missing"`
}

// StrandedName is one name a stranded task uses, and why it can no longer act.
type StrandedName struct {
	Name string `json:"name"`
	// Kind is "person" or "group".
	Kind string `json:"kind"`
	// Why is "gone" — no account or person of that name, no group of that
	// name — "inactive" for a deactivated person, or "empty" for a group with
	// nobody in it who can act.
	Why string `json:"why"`
}
