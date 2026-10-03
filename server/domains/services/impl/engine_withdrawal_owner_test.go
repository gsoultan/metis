package impl

import (
	"testing"

	"github.com/gsoultan/metis/server/repositories/models"
)

// Who else a withdrawal concerns is decided by the one rule for "waiting to be
// handed back" — delegated, owned and pending, all three — and not by the
// owner column alone, which a task can carry after it stopped being delegated.
func TestOnlyATaskWaitingToBeHandedBackHasAnOwnerToTell(t *testing.T) {
	for _, c := range []struct {
		name string
		row  models.TaskModel
		want string
	}{
		{"with a delegate", models.TaskModel{Status: models.TaskDelegated, Assignee: "dita", Owner: "ollie", DelegationState: "pending"}, "ollie"},
		{"never delegated", models.TaskModel{Status: models.TaskClaimed, Assignee: "dita"}, ""},
		{"handed back", models.TaskModel{Status: models.TaskClaimed, Assignee: "ollie", Owner: "ollie", DelegationState: "resolved"}, ""},
		{"a stale mark on a claimed task", models.TaskModel{Status: models.TaskClaimed, Assignee: "dita", Owner: "ollie", DelegationState: "pending"}, ""},
		{"delegated before owners were kept", models.TaskModel{Status: models.TaskDelegated, Assignee: "dita"}, ""},
		{"delegated and owned but not pending", models.TaskModel{Status: models.TaskDelegated, Assignee: "dita", Owner: "ollie"}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := ownerAwaitingHandBack(c.row); got != c.want {
				t.Fatalf("the owner to tell is %q, want %q", got, c.want)
			}
		})
	}
}
