package task_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// notificationTitles is every notification somebody has been sent, oldest
// first, by its title.
func (h *taskHarness) notificationTitles(t *testing.T, username string) []string {
	t.Helper()
	var titles []string
	if err := h.db.Raw(`SELECT title FROM notifications WHERE user_id = ? ORDER BY created_at, id`, username).
		Scan(&titles).Error; err != nil {
		t.Fatalf("read %s's notifications: %v", username, err)
	}
	return titles
}

func TestWhoeverATaskIsHandedToIsTold(t *testing.T) {
	h := newTaskHarness(t)
	taskID := h.openTask(t, heldByAlice())
	path := "/api/v1/tasks/" + taskID

	// The pin: what the notifier already did is what it still does. The
	// process gave alice the task, and she was told once, in the words she
	// always was.
	if got := h.notificationTitles(t, "alice"); !slices.Equal(got, []string{"A task is waiting for you"}) {
		t.Fatalf("the process gave alice a task and she was sent %v; want the one notice a new task has always sent", got)
	}

	// A refused hand-over tells nobody anything.
	if status, _ := h.post(t, h.tokens["mallory"], path+"/assign", map[string]any{"user_id": "mallory", "reason": "mine"}); status != http.StatusForbidden {
		t.Fatalf("mallory taking alice's task: got %d, want 403", status)
	}
	if got := h.notificationTitles(t, "mallory"); len(got) != 0 {
		t.Fatalf("mallory was refused the task and was sent %v", got)
	}

	// Delegated: the delegate is told it is with them.
	if status, reply := h.post(t, h.tokens["alice"], path+"/delegate", map[string]any{"user_id": "mallory"}); status != http.StatusOK {
		t.Fatalf("alice delegating: %d (%s)", status, strings.TrimSpace(reply))
	}
	if got := h.notificationTitles(t, "mallory"); !slices.Contains(got, "A task was delegated to you") {
		t.Fatalf("mallory was delegated a task and was sent %v", got)
	}

	// Handed back: the owner is told it is theirs to complete.
	before := len(h.notificationTitles(t, "alice"))
	if status, reply := h.post(t, h.tokens["mallory"], path+"/resolve", map[string]any{}); status != http.StatusOK {
		t.Fatalf("mallory handing back: %d (%s)", status, strings.TrimSpace(reply))
	}
	got := h.notificationTitles(t, "alice")
	if len(got) != before+1 || got[len(got)-1] != "A task was handed back to you" {
		t.Fatalf("alice's task was handed back and she has been sent %v", got)
	}

	// Assigned: the person it goes to is told, and the person it left is not.
	before = len(h.notificationTitles(t, "alice"))
	told := len(h.notificationTitles(t, "mallory"))
	if status, reply := h.post(t, h.tokens["alice"], path+"/assign", map[string]any{"user_id": "mallory"}); status != http.StatusOK {
		t.Fatalf("alice assigning to mallory: %d (%s)", status, strings.TrimSpace(reply))
	}
	if after := len(h.notificationTitles(t, "mallory")); after != told+1 {
		t.Fatalf("mallory was assigned the task and went from %d notifications to %d", told, after)
	}
	if after := len(h.notificationTitles(t, "alice")); after != before {
		t.Fatalf("alice gave her task away and was sent %d more notifications about it", after-before)
	}
}
