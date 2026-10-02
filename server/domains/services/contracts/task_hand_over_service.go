package contracts

import (
	"context"

	"github.com/google/uuid"
)

// TaskHandOverService moves a task between people. Every method names who is
// asking and may carry why; each decides, holding the task's row, whether that
// person may, and refuses with a reason a person can act on.
type TaskHandOverService interface {
	// AssignTask gives the task to change.Target, who holds it from then on.
	AssignTask(ctx context.Context, id uuid.UUID, change HandOver) error
	// DelegateTask hands the task to change.Target to work on.
	DelegateTask(ctx context.Context, id uuid.UUID, change HandOver) error
	// UnclaimTask puts a claimed task back for its candidates to claim.
	UnclaimTask(ctx context.Context, id uuid.UUID, change HandOver) error
}
