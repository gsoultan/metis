package entities

import (
	"time"

	"github.com/google/uuid"
)

// CompensatableActivity records a completed activity that can be undone as part
// of a Saga compensation. Activities are tracked in reverse-completion order so
// they can be rolled back in the correct sequence.
type CompensatableActivity struct {
	// ID is the unique record ID.
	ID uuid.UUID
	// Instance is the process instance this activity belongs to.
	Instance *ProcessInstance
	// Node is the BPMN node of the completed activity.
	Node *Node
	// CompensationNode is the BPMN node of the compensation boundary event or task.
	CompensationNode *Node
	// Variables holds a snapshot of instance variables at the time of completion,
	// made available to the compensation task.
	Variables map[string]any
	// CompletedAt is when the forward activity completed.
	CompletedAt time.Time
	// Compensated indicates this activity has already been rolled back.
	Compensated bool
}
