package impl

import (
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// refuseClosedTask refuses to complete a task that is not open.
//
// A refusal, not a failure: the second of two submissions meets this, and so
// does somebody completing an approval the step no longer needed, and a 5xx
// would tell their client to send it again. The inbox shows the text to the
// person, so it names no id.
//
// Open is what the task's own state machine says may become completed — not
// "anything that is neither completed nor cancelled". A status this service
// has never heard of is not open: absent constraint means deny.
func refuseClosedTask(status entities.TaskStatus) error {
	switch {
	case status == entities.TaskCompleted:
		return apierr.Invalidf("this task is already completed")
	case status == entities.TaskCanceled:
		return apierr.Invalidf("this task was withdrawn because its step no longer needs it, so it cannot be completed")
	case !status.CanTransitionTo(entities.TaskCompleted):
		return apierr.Invalidf("this task is not open, so it cannot be completed")
	}
	return nil
}
