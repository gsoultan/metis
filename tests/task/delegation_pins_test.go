package task_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// The pins for the refusal a pending delegation puts inside CompleteTask, which
// every completion passes through. They were written and run before that
// refusal existed, and say what it must leave alone.

// A task nobody delegated is completed by its holder and by nobody else, and
// a second completion is told the task is completed: three answers, none of
// them new.
func TestCompletingATaskNobodyDelegatedIsAsItWas(t *testing.T) {
	h := newTaskHarness(t)
	taskID := h.openTask(t, heldByAlice())

	if status, reply := h.complete(t, "mallory", taskID, map[string]any{"approved": true}); status != http.StatusForbidden || strings.Contains(reply, "hand") {
		t.Fatalf("mallory completing alice's task: got %d (%s); want the 403 it has always been", status, strings.TrimSpace(reply))
	}
	if status, reply := h.complete(t, "alice", taskID, map[string]any{"approved": true}); status != http.StatusOK {
		t.Fatalf("alice completing her own task: got %d (%s), want 200", status, strings.TrimSpace(reply))
	}
	status, reply := h.complete(t, "alice", taskID, map[string]any{"approved": true})
	if status != http.StatusBadRequest || !strings.Contains(reply, "already completed") {
		t.Fatalf("alice completing it a second time: got %d (%s); want 400 saying it is already completed", status, strings.TrimSpace(reply))
	}
}

// Whether a task is closed is asked before whose it is. A delegated task the
// engine withdrew is told it was withdrawn, whoever asks.
func TestAWithdrawnDelegatedTaskIsToldItWasWithdrawnWhoeverAsks(t *testing.T) {
	h := newTaskHarness(t)
	taskID := h.openTask(t, heldByAlice())
	if status, reply := h.post(t, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/delegate", map[string]any{"user_id": "mallory"}); status != http.StatusOK {
		t.Fatalf("alice delegating to mallory: %d (%s)", status, strings.TrimSpace(reply))
	}
	if err := h.db.Exec(`UPDATE tasks SET status = 'canceled' WHERE id = ?`, taskID).Error; err != nil {
		t.Fatalf("withdraw the task as the engine does: %v", err)
	}
	for _, who := range []string{"mallory", "alice"} {
		status, reply := h.complete(t, who, taskID, map[string]any{"approved": true})
		if status != http.StatusBadRequest || !strings.Contains(reply, "withdrawn") || strings.Contains(reply, "hand it back") {
			t.Fatalf("%s completing a withdrawn delegation: got %d (%s); want 400 saying it was withdrawn", who, status, strings.TrimSpace(reply))
		}
	}
}

// A delegated task is somebody's: nobody claims it.
func TestADelegatedTaskIsAlreadyClaimedToAnybodyWhoTries(t *testing.T) {
	h := newTaskHarness(t)
	h.tokens["bob"] = h.signInWithRoles(t, "bob", entities.RoleUser)
	taskID := h.openTask(t, heldByAlice())
	if status, reply := h.post(t, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/delegate", map[string]any{"user_id": "mallory"}); status != http.StatusOK {
		t.Fatalf("alice delegating to mallory: %d (%s)", status, strings.TrimSpace(reply))
	}
	for _, who := range []string{"alice", "mallory", "bob"} {
		status, reply := h.post(t, h.tokens[who], "/api/v1/tasks/"+taskID+"/claim", map[string]any{})
		if status != http.StatusBadRequest || !strings.Contains(reply, "already claimed") {
			t.Fatalf("%s claiming a delegated task: got %d (%s); want 400 saying it is already claimed", who, status, strings.TrimSpace(reply))
		}
	}
}

// A pod still on the release before this one does not know a task has an
// owner. When it claims or releases a task this release delegated, the row
// keeps its owner and its pending mark under a status that is no longer
// "delegated". Such a row is not waiting for anybody: it is its holder's to
// complete, as every claimed task is.
func TestAStalePendingMarkOnATaskThatIsNoLongerDelegatedBlocksNothing(t *testing.T) {
	h := newTaskHarness(t)
	taskID := h.openTask(t, heldByAlice())
	if err := h.db.Exec(`UPDATE tasks SET owner = 'mallory', delegation_state = 'pending' WHERE id = ?`, taskID).Error; err != nil {
		t.Fatalf("leave the row as an old pod would: %v", err)
	}
	if got := h.taskStatus(t, taskID); got != "claimed" {
		t.Fatalf("the task is %q, want claimed", got)
	}

	if status, reply := h.complete(t, "mallory", taskID, map[string]any{"approved": true}); status != http.StatusForbidden {
		t.Fatalf("mallory, the stale owner, completing alice's task: got %d (%s), want 403", status, strings.TrimSpace(reply))
	}
	if status, reply := h.complete(t, "alice", taskID, map[string]any{"approved": true}); status != http.StatusOK {
		t.Fatalf("alice completing the task she holds: got %d (%s), want 200", status, strings.TrimSpace(reply))
	}
}
