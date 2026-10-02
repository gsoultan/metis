package contracts

import "time"

// TaskEdit changes a task's name, priority or due date. A field left nil is
// left as it is; ClearDueDate removes the due date.
type TaskEdit struct {
	// Actor is the signed-in caller making the change, from the verified token.
	Actor string
	// Reason is why. Required unless Actor holds the task.
	Reason string

	Name     *string
	Priority *int
	// DueDate sets the due date. Ignored when ClearDueDate is set.
	DueDate      *time.Time
	ClearDueDate bool
}
