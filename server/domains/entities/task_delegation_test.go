package entities_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A delegation is pending only when the task is delegated, has an owner and is
// marked pending — all three. Anything else carrying an owner or a state is
// either a task that came back to its owner or a leftover.
func TestTask_WhetherADelegationIsPendingOrStale(t *testing.T) {
	user := func(name string) *entities.User {
		if name == "" {
			return nil
		}
		return &entities.User{Username: name}
	}
	for _, tc := range []struct {
		name            string
		status          entities.TaskStatus
		assignee, owner string
		state           entities.DelegationState
		awaits, stale   bool
	}{
		{"with its delegate", entities.TaskDelegated, "citra", "budi", entities.DelegationPending, true, false},
		{"back with its owner", entities.TaskClaimed, "budi", "budi", entities.DelegationResolved, false, false},
		{"never delegated", entities.TaskClaimed, "budi", "", "", false, false},
		{"delegated by a release that kept no owner", entities.TaskDelegated, "citra", "", "", false, false},
		{"claimed over a pending delegation by an old pod", entities.TaskClaimed, "citra", "budi", entities.DelegationPending, false, true},
		{"released over a pending delegation by an old pod", entities.TaskUnclaimed, "", "budi", entities.DelegationPending, false, true},
		{"withdrawn while with its delegate", entities.TaskCanceled, "citra", "budi", entities.DelegationPending, false, true},
		{"pending with nobody to go back to", entities.TaskDelegated, "citra", "", entities.DelegationPending, false, true},
		{"came back and was handed on by an old pod", entities.TaskClaimed, "dita", "budi", entities.DelegationResolved, false, true},
		{"came back and was delegated again by an old pod", entities.TaskDelegated, "dita", "budi", entities.DelegationResolved, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := entities.Task{Status: tc.status, Assignee: user(tc.assignee), Owner: user(tc.owner), DelegationState: tc.state}
			if got := task.AwaitsHandBack(); got != tc.awaits {
				t.Errorf("AwaitsHandBack() = %v, want %v", got, tc.awaits)
			}
			if got := task.HasStaleDelegation(); got != tc.stale {
				t.Errorf("HasStaleDelegation() = %v, want %v", got, tc.stale)
			}
		})
	}
}
