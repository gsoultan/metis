package entities

import "github.com/google/uuid"

// DeviationOpenWork is one task that is somebody's to do where a deviation
// would act: what a plan shows an administrator before the work is taken from
// whoever has it.
//
// No JSON or ORM tags: an endpoint maps it to a view.
type DeviationOpenWork struct {
	TaskID uuid.UUID
	Name   string
	// NodeID and NodeName are the step the task is on, the name falling back
	// to the id. A cancel lists the open work of every step of the instance.
	NodeID, NodeName string
	Status           TaskStatus
	// Assignee is who holds the task, or empty when nobody does.
	Assignee string
	// IterationID names the run of a repeating step the task is for, and is
	// empty on a step that runs once.
	IterationID string
}
