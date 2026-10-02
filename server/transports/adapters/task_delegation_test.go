package adapters

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// The inbox reads its tasks over Connect. What it is not sent it cannot show:
// who a task was delegated by, and that it has to be handed back.
func TestTaskPBAdapter_CarriesWhoATaskGoesBackTo(t *testing.T) {
	message := TaskPBAdapter{Task: entities.Task{
		Name: "Approve the refund", Status: entities.TaskDelegated,
		Assignee:        &entities.User{Username: "citra"},
		Owner:           &entities.User{Username: "budi"},
		DelegationState: entities.DelegationPending,
	}}.ToProto()
	if message.GetOwner().GetUsername() != "budi" || message.GetDelegationState() != "pending" {
		t.Fatalf("sent owner %q, state %q; want budi, pending", message.GetOwner().GetUsername(), message.GetDelegationState())
	}

	plain := TaskPBAdapter{Task: entities.Task{Name: "Approve the refund", Status: entities.TaskClaimed}}.ToProto()
	if plain.GetOwner() != nil || plain.GetDelegationState() != "" {
		t.Fatalf("a task never delegated was sent owner %v, state %q", plain.GetOwner(), plain.GetDelegationState())
	}
}
