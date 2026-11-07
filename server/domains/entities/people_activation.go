package entities

import (
	"time"

	"github.com/google/uuid"
)

// What deactivating somebody does with the open tasks they hold.
const (
	// HeldWorkKeep leaves them with the person. Nobody can act on them until
	// somebody hands them over; they are listed as stranded until then.
	HeldWorkKeep = "keep"
	// HeldWorkRelease puts them back on offer, unclaimed, to whoever the step
	// offers them to.
	HeldWorkRelease = "release"
	// HeldWorkHandOver gives them to one named account.
	HeldWorkHandOver = "hand_over"
)

// HeldWork is what to do with the open tasks a person holds when they are
// deactivated.
type HeldWork struct {
	// Do is one of the HeldWork* constants. Empty means HeldWorkKeep.
	Do string `json:"do,omitzero"`
	// To is the account the tasks go to, for HeldWorkHandOver.
	To string `json:"to,omitzero"`
	// Reason is why, kept on each task's history. Required to move a task:
	// whoever deactivates somebody does not hold their work.
	Reason string `json:"reason,omitzero"`
}

// HeldTask is one open task a person holds.
type HeldTask struct {
	TaskID     uuid.UUID `json:"task_id"`
	Name       string    `json:"name"`
	InstanceID uuid.UUID `json:"instance_id,omitzero"`
	Since      time.Time `json:"since,omitzero"`
}

// ActivationResult is what changing whether somebody is active did.
type ActivationResult struct {
	Participant WorkflowUser `json:"participant"`
	// Moved counts the held tasks handed over or released.
	Moved int `json:"moved"`
	// Kept are the held tasks still with the person: all of them when the
	// work was kept, and any a move was refused for, with why.
	Kept []KeptTask `json:"kept,omitzero"`
}

// KeptTask is a held task that stayed with a deactivated person.
type KeptTask struct {
	HeldTask
	// Why it was not moved, when a move was asked for and refused.
	Why string `json:"why,omitzero"`
}
