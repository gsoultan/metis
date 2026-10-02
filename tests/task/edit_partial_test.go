package task_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// taskFields reads back the three things an edit can change. The due date is
// as the server writes it, "" for none.
func (h *taskHarness) taskFields(t *testing.T, id string) (string, int, string) {
	t.Helper()
	status, body := h.get(t, h.tokens["alice"], "/api/v1/tasks/"+id)
	if status != http.StatusOK {
		t.Fatalf("get task: status %d (%s)", status, body)
	}
	var out struct {
		Task struct {
			Name     string `json:"name"`
			Priority int    `json:"priority"`
			DueDate  string `json:"due_date"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return out.Task.Name, out.Task.Priority, out.Task.DueDate
}

func sameInstant(t *testing.T, got, want string) bool {
	t.Helper()
	if got == "" || want == "" {
		return got == want
	}
	a, err := time.Parse(time.RFC3339Nano, got)
	if err != nil {
		t.Fatalf("the task's due date %q is not a time: %v", got, err)
	}
	b, err := time.Parse(time.RFC3339Nano, want)
	if err != nil {
		t.Fatalf("%q is not a time: %v", want, err)
	}
	return a.Equal(b)
}

// PUT /api/v1/tasks/{id} wrote all three fields from whatever the request
// held, so a request carrying only a due date set the name to "" and the
// priority to 0.
func TestAnEditChangesOnlyTheFieldsItCarries(t *testing.T) {
	h := newTaskHarness(t)
	taskID, instanceID := h.openTaskWith(t, heldByAlice(), nil)
	put := func(body map[string]any) {
		t.Helper()
		if status, reply := h.do(t, http.MethodPut, h.tokens["alice"], "/api/v1/tasks/"+taskID, body); status != http.StatusOK {
			t.Fatalf("PUT %v: got %d (%s)", body, status, strings.TrimSpace(reply))
		}
	}
	expect := func(after, wantName string, wantPriority int, wantDue string) {
		t.Helper()
		name, priority, due := h.taskFields(t, taskID)
		if name != wantName || priority != wantPriority || !sameInstant(t, due, wantDue) {
			t.Fatalf("after %s the task is %q at priority %d, due %q; want %q at %d, due %q",
				after, name, priority, due, wantName, wantPriority, wantDue)
		}
	}
	const december = "2026-12-01T09:00:00Z"

	put(map[string]any{"name": "Approve the refund today", "priority": 50})
	expect("naming it and setting its priority", "Approve the refund today", 50, "")

	put(map[string]any{"due_date": december})
	expect("an edit carrying only a due date", "Approve the refund today", 50, december)

	put(map[string]any{"priority": 70})
	expect("an edit carrying only a priority", "Approve the refund today", 70, december)

	put(map[string]any{"due_date": nil})
	expect("an edit carrying a null due date", "Approve the refund today", 70, "")

	put(map[string]any{"due_date": december})
	put(map[string]any{"name": "Approve the refund today", "priority": 70, "due_date": ""})
	expect("the inbox saving a task whose due date was cleared", "Approve the refund today", 70, "")

	// Each edit left an entry saying who changed which field, from what, to
	// what — and only the fields that changed.
	edits := h.trail(t, instanceID, "task_edited")
	if len(edits) != 6 {
		t.Fatalf("six edits changed something and the trail has %d task_edited entries", len(edits))
	}
	second := edits[1]
	changes, _ := second.Data["changes"].(map[string]any)
	due, _ := changes["due_date"].(map[string]any)
	if second.Data["actor"] != "alice" || len(changes) != 1 || due["before"] != nil || due["after"] != december {
		t.Errorf("the edit that set the due date is recorded as %v; want actor alice and due_date from nothing to %s, and nothing else", second.Data, december)
	}
	if want := `alice changed the due date of task "Approve the refund today"`; second.Narrative != want {
		t.Errorf("it reads %q, want %q", second.Narrative, want)
	}
	first, _ := edits[0].Data["changes"].(map[string]any)
	name, _ := first["name"].(map[string]any)
	priority, _ := first["priority"].(map[string]any)
	if name["before"] != "Approve the refund" || name["after"] != "Approve the refund today" || priority["before"] != float64(0) || priority["after"] != float64(50) {
		t.Errorf("the first edit's changes are recorded as %v; want the name and the priority, each before and after", first)
	}
}

// Review focus 3. The inbox holds a due date as the text it was sent, cut to
// whole seconds, and sends it back with every save. Compared to the instant
// stored, that read as a change: an entry for an edit nobody made, and the
// stored value quietly losing its precision.
func TestResendingAnUnchangedDueDateChangesNothing(t *testing.T) {
	h := newTaskHarness(t)
	step := heldByAlice()
	step.DueDate = "PT2H"
	taskID, instanceID := h.openTaskWith(t, step, nil)
	name, priority, due := h.taskFields(t, taskID)
	if due == "" {
		t.Fatal("the step has a due date of two hours and its task has none")
	}
	stored, err := time.Parse(time.RFC3339Nano, due)
	if err != nil {
		t.Fatalf("the task's due date %q is not a time: %v", due, err)
	}

	status, reply := h.do(t, http.MethodPut, h.tokens["alice"], "/api/v1/tasks/"+taskID,
		map[string]any{"name": name, "priority": priority, "due_date": stored.Format(time.RFC3339)})
	if status != http.StatusOK {
		t.Fatalf("the inbox saving a task it did not change: got %d (%s)", status, strings.TrimSpace(reply))
	}
	if _, _, after := h.taskFields(t, taskID); after != due {
		t.Fatalf("the due date was %q and is now %q; nothing changed it", due, after)
	}
	if edits := h.trail(t, instanceID, "task_edited"); len(edits) != 0 {
		t.Fatalf("nothing was changed and the trail has %d edit entries: %v", len(edits), edits[0].Data)
	}
}

// The denials: who may not edit, and what an edit may not be.
func TestAnEditIsRefusedToAStrangerAndToAnAdministratorWhoDoesNotSayWhy(t *testing.T) {
	h := newTaskHarness(t)
	boss := h.signInAdministrator(t, "boss")
	taskID, instanceID := h.openTaskWith(t, heldByAlice(), nil)
	path := "/api/v1/tasks/" + taskID

	if status, reply := h.do(t, http.MethodPut, h.tokens["mallory"], path, map[string]any{"priority": 1, "reason": "it can wait"}); status != http.StatusForbidden {
		t.Fatalf("mallory editing alice's task: got %d (%s), want 403", status, strings.TrimSpace(reply))
	}
	status, reply := h.do(t, http.MethodPut, boss, path, map[string]any{"priority": 90})
	if status != http.StatusBadRequest || !strings.Contains(reply, "say why") {
		t.Fatalf("an administrator editing alice's task with no reason: got %d (%s), want 400 asking why", status, strings.TrimSpace(reply))
	}
	for what, body := range map[string]map[string]any{
		"a due date that is not a time": {"due_date": "tomorrow"},
		"a name of spaces":              {"name": "   "},
	} {
		if status, reply := h.do(t, http.MethodPut, h.tokens["alice"], path, body); status != http.StatusBadRequest {
			t.Fatalf("an edit carrying %s: got %d (%s), want 400", what, status, strings.TrimSpace(reply))
		}
	}
	if name, priority, due := h.taskFields(t, taskID); name != "Approve the refund" || priority != 0 || due != "" {
		t.Fatalf("after the refusals the task is %q at priority %d, due %q; want it as it was", name, priority, due)
	}

	if status, reply := h.do(t, http.MethodPut, boss, path, map[string]any{"priority": 90, "reason": "the customer escalated"}); status != http.StatusOK {
		t.Fatalf("an administrator editing alice's task with a reason: got %d (%s)", status, strings.TrimSpace(reply))
	}
	entry := h.lastOf(t, instanceID, "task_edited")
	if entry.Data["actor"] != "boss" || entry.Data["holder"] != "alice" || entry.Data["reason"] != "the customer escalated" {
		t.Errorf("the administrator's edit is recorded as %v; want actor boss, holder alice and the reason", entry.Data)
	}

	// A finished task is history, not something to rename.
	if status, reply := h.complete(t, "alice", taskID, map[string]any{"approved": true}); status != http.StatusOK {
		t.Fatalf("alice completing: %d (%s)", status, reply)
	}
	if status, reply := h.do(t, http.MethodPut, h.tokens["alice"], path, map[string]any{"name": "Something else"}); status != http.StatusBadRequest {
		t.Fatalf("editing a completed task: got %d (%s), want 400", status, strings.TrimSpace(reply))
	}
}
