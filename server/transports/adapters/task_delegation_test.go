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

// A row can carry an owner and a pending mark under a status that is not
// "delegated": a pod on the release before this one claimed it, or the engine
// withdrew it. It is not waiting to be handed back, and the inbox is not left
// to work that out.
func TestTaskPBAdapter_SendsNoPendingMarkForATaskThatIsNotWaitingToBeHandedBack(t *testing.T) {
	for name, status := range map[string]entities.TaskStatus{
		"claimed by an old pod":             entities.TaskClaimed,
		"withdrawn while with its delegate": entities.TaskCanceled,
	} {
		t.Run(name, func(t *testing.T) {
			message := TaskPBAdapter{Task: entities.Task{
				Name: "Approve the refund", Status: status,
				Assignee:        &entities.User{Username: "citra"},
				Owner:           &entities.User{Username: "budi"},
				DelegationState: entities.DelegationPending,
			}}.ToProto()
			if message.GetOwner() != nil || message.GetDelegationState() != "" {
				t.Fatalf("sent owner %q, state %q for a task that is %s", message.GetOwner().GetUsername(), message.GetDelegationState(), status)
			}
			if message.GetAssignee().GetUsername() != "citra" || message.GetStatus() != string(status) {
				t.Fatalf("sent assignee %q, status %q; want citra, %s", message.GetAssignee().GetUsername(), message.GetStatus(), status)
			}
		})
	}
}

// The pin: a delegation that was handed back is sent with who it came back to.
func TestTaskPBAdapter_KeepsAHandedBackDelegation(t *testing.T) {
	message := TaskPBAdapter{Task: entities.Task{
		Name: "Approve the refund", Status: entities.TaskClaimed,
		Assignee:        &entities.User{Username: "budi"},
		Owner:           &entities.User{Username: "budi"},
		DelegationState: entities.DelegationResolved,
	}}.ToProto()
	if message.GetOwner().GetUsername() != "budi" || message.GetDelegationState() != "resolved" {
		t.Fatalf("sent owner %q, state %q; want budi, resolved", message.GetOwner().GetUsername(), message.GetDelegationState())
	}
}
