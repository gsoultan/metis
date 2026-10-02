package impl

import (
	"testing"

	"github.com/gsoultan/metis/server/repositories/models"
)

// Review focus 5. A migration re-derives who may do a task it moves, and whoever
// held it claims it again. A delegation is a claim with a way back; moved onto
// another step with its owner and its pending state left on, it would be a task
// nobody may complete until a delegate who no longer holds it hands it back.
func TestAMigratedTaskLosesItsDelegation(t *testing.T) {
	t.Parallel()
	delegated := models.TaskModel{
		NodeID: "approve", Status: models.TaskDelegated, Assignee: "citra",
		Owner: "budi", DelegationState: "pending",
	}

	offered := retargetTask(delegated, models.FlowNode{ID: "review", Name: "Review", CandidateUsers: []string{"budi"}})
	if offered.Owner != "" || offered.DelegationState != "" || offered.Assignee != "" || offered.Status != models.TaskUnclaimed {
		t.Fatalf("moved onto a step offered to people, the task is %s, held by %q, owned by %q, delegation %q; want unclaimed with none of them",
			offered.Status, offered.Assignee, offered.Owner, offered.DelegationState)
	}

	given := retargetTask(delegated, models.FlowNode{ID: "review", Name: "Review", Assignee: "dita"})
	if given.Owner != "" || given.DelegationState != "" || given.Assignee != "dita" || given.Status != models.TaskClaimed {
		t.Fatalf("moved onto a step given to dita, the task is %s, held by %q, owned by %q, delegation %q; want claimed by dita and no delegation",
			given.Status, given.Assignee, given.Owner, given.DelegationState)
	}
}
