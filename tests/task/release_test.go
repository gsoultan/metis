package task_test

import (
	"net/http"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Releasing a task puts it back in the queue for anyone to claim, and the
// endpoint asked nobody's permission: any member of the organization could
// release a task somebody else was holding — one assigned to them directly
// included — and then claim it for themselves.
func TestOnlyTheHolderMayReleaseATask(t *testing.T) {
	h := newTaskHarness(t)
	taskID := h.assignTaskTo(t, "alice")

	status, body := h.post(t, h.tokens["mallory"], "/api/v1/tasks/"+taskID+"/unclaim", map[string]any{})
	if status != http.StatusForbidden {
		t.Fatalf("mallory releasing alice's task: got %d (%s), want 403", status, body)
	}
	if got := h.taskStatus(t, taskID); got != string(entities.TaskClaimed) {
		t.Fatalf("after the refused release the task is %q, want it still claimed by alice", got)
	}

	status, body = h.post(t, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/unclaim", map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("alice releasing her own task: got %d (%s)", status, body)
	}
	if got := h.taskStatus(t, taskID); got != string(entities.TaskUnclaimed) {
		t.Fatalf("after alice released it the task is %q, want unclaimed", got)
	}
}
