package task_test

import (
	"net/http"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Delegating or assigning a task set its status without asking what it was,
// so a completed task could be handed on — reopened — and completed again,
// running everything after it a second time: a payment step, an email, a
// call to a partner.
func TestAClosedTaskCannotBeHandedOnOrCompletedAgain(t *testing.T) {
	h := newTaskHarness(t)
	taskID := h.assignTaskTo(t, "alice")

	if status, body := h.post(t, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/complete",
		map[string]any{"variables": map[string]any{"approved": true}}); status != http.StatusOK {
		t.Fatalf("alice completing her task: %d (%s)", status, body)
	}

	for _, action := range []string{"delegate", "assign"} {
		status, body := h.post(t, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/"+action, map[string]any{"user_id": "mallory"})
		if status == http.StatusOK {
			t.Errorf("a completed task was %sd to mallory: %s", action, body)
		}
		if got := h.taskStatus(t, taskID); got != string(entities.TaskCompleted) {
			t.Fatalf("after a refused %s the task is %q, want it still completed", action, got)
		}
	}
	if status, body := h.post(t, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/complete",
		map[string]any{"variables": map[string]any{}}); status == http.StatusOK {
		t.Errorf("a completed task was completed a second time: %s", body)
	}
}
