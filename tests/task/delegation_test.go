package task_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// delegation reads back who holds a task, who it goes back to, and where the
// delegation stands.
func (h *taskHarness) delegation(t *testing.T, id string) (string, string, string) {
	t.Helper()
	status, body := h.get(t, h.tokens["alice"], "/api/v1/tasks/"+id)
	if status != http.StatusOK {
		t.Fatalf("get task: status %d (%s)", status, body)
	}
	var out struct {
		Task struct {
			Assignee struct {
				Username string `json:"username"`
			} `json:"assignee"`
			Owner struct {
				Username string `json:"username"`
			} `json:"owner"`
			DelegationState string `json:"delegation_state"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return out.Task.Assignee.Username, out.Task.Owner.Username, out.Task.DelegationState
}

// delegatedBy lists the ids of the tasks somebody delegated that are still
// with their delegate.
func (h *taskHarness) delegatedBy(t *testing.T, who string) []string {
	t.Helper()
	status, body := h.get(t, h.tokens[who], "/api/v1/tasks/delegated?page=1&page_size=50")
	if status != http.StatusOK {
		t.Fatalf("list what %s delegated: status %d (%s)", who, status, body)
	}
	var out struct {
		Tasks []struct {
			ID string `json:"id"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode what %s delegated: %v", who, err)
	}
	ids := make([]string, 0, len(out.Tasks))
	for _, task := range out.Tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

// delegatedToMallory is alice's task, delegated by her to mallory.
func (h *taskHarness) delegatedToMallory(t *testing.T) string {
	t.Helper()
	taskID := h.openTask(t, heldByAlice())
	if status, reply := h.post(t, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/delegate", map[string]any{"user_id": "mallory"}); status != http.StatusOK {
		t.Fatalf("alice delegating to mallory: %d (%s)", status, strings.TrimSpace(reply))
	}
	return taskID
}

// Delegating made the delegate the task's assignee and kept nothing else: the
// delegate could complete an approval that was never theirs to give, and the
// person it was given to could not get it back. The holder stays its owner
// now; the delegate does the work and hands it back; the owner completes it.
func TestADelegatedTaskGoesBackToItsOwnerWhoCompletesIt(t *testing.T) {
	h := newTaskHarness(t)
	taskID, instanceID := h.openTaskWith(t, heldByAlice(), nil)
	path := "/api/v1/tasks/" + taskID

	if status, reply := h.post(t, h.tokens["alice"], path+"/delegate", map[string]any{"user_id": "mallory"}); status != http.StatusOK {
		t.Fatalf("alice delegating to mallory: %d (%s)", status, strings.TrimSpace(reply))
	}
	if assignee, owner, state := h.delegation(t, taskID); assignee != "mallory" || owner != "alice" || state != "pending" || h.taskStatus(t, taskID) != "delegated" {
		t.Fatalf("delegated, the task is with %q, owned by %q, %q, status %s; want mallory, alice, pending, delegated",
			assignee, owner, state, h.taskStatus(t, taskID))
	}
	if entry := h.lastOf(t, instanceID, "task_delegated"); entry.Data["owner"] != "alice" || entry.Data["target"] != "mallory" || entry.Data["actor"] != "alice" {
		t.Errorf("the delegation is recorded as %v; want actor alice, owner alice, target mallory", entry.Data)
	}

	// While it is with the delegate, nobody completes it.
	status, reply := h.complete(t, "mallory", taskID, map[string]any{"approved": true})
	if status != http.StatusForbidden || !strings.Contains(reply, "hand it back to alice first") {
		t.Fatalf("mallory completing a task delegated to her: got %d (%s); want 403 telling her to hand it back to alice first", status, strings.TrimSpace(reply))
	}
	status, reply = h.complete(t, "alice", taskID, map[string]any{"approved": true})
	if status != http.StatusForbidden || !strings.Contains(reply, "is with mallory") {
		t.Fatalf("alice completing a task that is with her delegate: got %d (%s); want 403 saying it is with mallory", status, strings.TrimSpace(reply))
	}
	if got := h.taskStatus(t, taskID); got != "delegated" {
		t.Fatalf("after the refused completions the task is %q", got)
	}

	// The delegate hands it back, with no reason asked: they hold it.
	if status, reply := h.raw(t, http.MethodPost, h.tokens["mallory"], path+"/resolve", ""); status != http.StatusOK {
		t.Fatalf("mallory handing the task back: %d (%s)", status, strings.TrimSpace(reply))
	}
	if assignee, owner, state := h.delegation(t, taskID); assignee != "alice" || owner != "alice" || state != "resolved" || h.taskStatus(t, taskID) != "claimed" {
		t.Fatalf("handed back, the task is with %q, owned by %q, %q, status %s; want alice, alice, resolved, claimed",
			assignee, owner, state, h.taskStatus(t, taskID))
	}
	resolved := h.lastOf(t, instanceID, "task_resolved")
	if resolved.Data["actor"] != "mallory" || resolved.Data["previous_holder"] != "mallory" || resolved.Data["target"] != "alice" {
		t.Errorf("the hand-back is recorded as %v; want actor mallory, previous_holder mallory, target alice", resolved.Data)
	}
	if want := `mallory handed task "Approve the refund" back to alice`; resolved.Narrative != want {
		t.Errorf("it reads %q, want %q", resolved.Narrative, want)
	}

	// And the owner completes as usual.
	if status, reply := h.complete(t, "mallory", taskID, map[string]any{"approved": true}); status != http.StatusForbidden {
		t.Fatalf("mallory completing after handing it back: got %d (%s), want 403", status, strings.TrimSpace(reply))
	}
	if status, reply := h.complete(t, "alice", taskID, map[string]any{"approved": true}); status != http.StatusOK {
		t.Fatalf("alice completing her task once it is back: got %d (%s)", status, strings.TrimSpace(reply))
	}
}

func TestDelegatingATaskNobodyHoldsIsRefused(t *testing.T) {
	h := newTaskHarness(t)
	boss := h.signInAdministrator(t, "boss")
	taskID := h.openTask(t, offeredToFinance())

	status, reply := h.post(t, boss, "/api/v1/tasks/"+taskID+"/delegate", map[string]any{"user_id": "alice", "reason": "alice knows the customer"})
	if status != http.StatusBadRequest || !strings.Contains(reply, "claim or assign it first") {
		t.Fatalf("delegating a task nobody holds: got %d (%s); want 400 saying to claim or assign it first", status, strings.TrimSpace(reply))
	}
	if assignee, owner, state := h.delegation(t, taskID); assignee != "" || owner != "" || state != "" {
		t.Fatalf("after the refusal the task is with %q, owned by %q, %q; want nobody", assignee, owner, state)
	}
}

// The denials for the new operation: who may not hand a task back.
func TestOnlyTheDelegateOrAnAdministratorWhoSaysWhyHandsATaskBack(t *testing.T) {
	h := newTaskHarness(t)
	boss := h.signInAdministrator(t, "boss")
	h.tokens["bob"] = h.signInWithRoles(t, "bob", entities.RoleUser)
	taskID, instanceID := h.openTaskWith(t, heldByAlice(), nil)
	path := "/api/v1/tasks/" + taskID + "/resolve"

	// A task that was never delegated has nothing to hand back.
	if status, reply := h.post(t, h.tokens["alice"], path, map[string]any{}); status != http.StatusBadRequest || !strings.Contains(reply, "nothing to hand back") {
		t.Fatalf("alice handing back her own, undelegated task: got %d (%s), want 400", status, strings.TrimSpace(reply))
	}
	if status, reply := h.post(t, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/delegate", map[string]any{"user_id": "mallory"}); status != http.StatusOK {
		t.Fatalf("alice delegating to mallory: %d (%s)", status, strings.TrimSpace(reply))
	}

	// The owner cannot take it back, and neither can a bystander: with or
	// without a reason.
	for _, who := range []string{"alice", "bob"} {
		if status, reply := h.post(t, h.tokens[who], path, map[string]any{"reason": "I want it back"}); status != http.StatusForbidden {
			t.Fatalf("%s handing back a task delegated to mallory: got %d (%s), want 403", who, status, strings.TrimSpace(reply))
		}
	}
	if status, reply := h.raw(t, http.MethodPost, "", path, ""); status != http.StatusUnauthorized {
		t.Fatalf("nobody signed in handing a task back: got %d (%s), want 401", status, strings.TrimSpace(reply))
	}
	status, reply := h.post(t, boss, path, map[string]any{})
	if status != http.StatusBadRequest || !strings.Contains(reply, "say why") {
		t.Fatalf("an administrator handing back mallory's delegation with no reason: got %d (%s); want 400 asking why", status, strings.TrimSpace(reply))
	}
	if assignee, _, state := h.delegation(t, taskID); assignee != "mallory" || state != "pending" {
		t.Fatalf("after the refusals the task is with %q, %q; want it still with mallory, pending", assignee, state)
	}

	if status, reply := h.post(t, boss, path, map[string]any{"reason": "mallory is away"}); status != http.StatusOK {
		t.Fatalf("an administrator handing back mallory's delegation with a reason: got %d (%s)", status, strings.TrimSpace(reply))
	}
	if assignee, _, state := h.delegation(t, taskID); assignee != "alice" || state != "resolved" {
		t.Fatalf("handed back by the administrator, the task is with %q, %q; want alice, resolved", assignee, state)
	}
	if want, got := `boss handed task "Approve the refund" back from mallory to alice: mallory is away`, h.lastOf(t, instanceID, "task_resolved").Narrative; got != want {
		t.Errorf("it reads %q, want %q", got, want)
	}
}

// An operator takes work nobody was named for. That is not a say over work
// somebody was: handing a delegation back for its delegate is an
// administrator's, and an operator's reason does not make it theirs.
func TestAnOperatorDoesNotHandBackSomebodyElsesDelegation(t *testing.T) {
	h := newTaskHarness(t)
	operator := h.signInWithRoles(t, "olga", entities.RoleOperator)
	taskID := h.delegatedToMallory(t)
	path := "/api/v1/tasks/" + taskID + "/resolve"

	for name, body := range map[string]map[string]any{
		"with a reason": {"reason": "mallory is away"},
		"with none":     {},
	} {
		status, reply := h.post(t, operator, path, body)
		if status != http.StatusForbidden || !strings.Contains(reply, "or an administrator") {
			t.Fatalf("an operator handing back mallory's delegation %s: got %d (%s); want 403 saying whose it is to hand back", name, status, strings.TrimSpace(reply))
		}
	}
	if assignee, owner, state := h.delegation(t, taskID); assignee != "mallory" || owner != "alice" || state != "pending" {
		t.Fatalf("after the refusals the task is with %q, owned by %q, %q; want mallory, alice, pending", assignee, owner, state)
	}
	if got := h.notificationTitles(t, "alice"); slices.Contains(got, "A task was handed back to you") {
		t.Fatalf("nothing was handed back and alice was sent %v", got)
	}
}

func TestAPendingDelegationIsHandedBackNotReleasedOrHandedOn(t *testing.T) {
	h := newTaskHarness(t)
	boss := h.signInAdministrator(t, "boss")
	h.tokens["bob"] = h.signInWithRoles(t, "bob", entities.RoleUser)
	taskID := h.delegatedToMallory(t)

	for action, body := range map[string]map[string]any{
		"unclaim":  {"reason": "not mine to do"},
		"assign":   {"user_id": "bob", "reason": "bob can do it"},
		"delegate": {"user_id": "bob", "reason": "bob can do it"},
	} {
		for who, token := range map[string]string{"mallory, the delegate": h.tokens["mallory"], "an administrator": boss} {
			status, reply := h.post(t, token, "/api/v1/tasks/"+taskID+"/"+action, body)
			if status != http.StatusBadRequest || !strings.Contains(reply, "handed back") {
				t.Fatalf("%s trying to %s a task that is waiting to be handed back: got %d (%s); want 400 saying it is handed back first",
					who, action, status, strings.TrimSpace(reply))
			}
		}
	}
	if assignee, owner, state := h.delegation(t, taskID); assignee != "mallory" || owner != "alice" || state != "pending" {
		t.Fatalf("after the refusals the task is with %q, owned by %q, %q; want mallory, alice, pending", assignee, owner, state)
	}
}

// Once it is back, it is an ordinary claim again: handing it to somebody else,
// or releasing it, ends the delegation rather than leaving its owner on a task
// they gave away.
func TestHandingOnOrReleasingATaskThatCameBackEndsItsDelegation(t *testing.T) {
	h := newTaskHarness(t)
	for action, body := range map[string]map[string]any{
		"assign":  {"user_id": "mallory"},
		"unclaim": {},
	} {
		taskID := h.delegatedToMallory(t)
		if status, reply := h.post(t, h.tokens["mallory"], "/api/v1/tasks/"+taskID+"/resolve", map[string]any{}); status != http.StatusOK {
			t.Fatalf("mallory handing it back: %d (%s)", status, strings.TrimSpace(reply))
		}
		if status, reply := h.post(t, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/"+action, body); status != http.StatusOK {
			t.Fatalf("alice trying to %s the task once it is back: %d (%s)", action, status, strings.TrimSpace(reply))
		}
		if _, owner, state := h.delegation(t, taskID); owner != "" || state != "" {
			t.Fatalf("after alice's %s the task is still owned by %q, delegation %q; want neither", action, owner, state)
		}
	}
}

// The owner's side of it, and the denial for the new listing: it is the
// caller's own, whoever they are, and nobody's without a caller.
func TestAnOwnerSeesWhatTheyDelegatedAndNobodyElseDoes(t *testing.T) {
	h := newTaskHarness(t)
	taskID := h.delegatedToMallory(t)

	if got := h.delegatedBy(t, "alice"); !slices.Equal(got, []string{taskID}) {
		t.Fatalf("alice delegated %s and is shown %v", taskID, got)
	}
	if got := h.delegatedBy(t, "mallory"); len(got) != 0 {
		t.Fatalf("mallory delegated nothing and is shown %v", got)
	}
	if status, reply := h.raw(t, http.MethodGet, "", "/api/v1/tasks/delegated", ""); status != http.StatusUnauthorized {
		t.Fatalf("nobody signed in listing what they delegated: got %d (%s), want 401", status, strings.TrimSpace(reply))
	}

	if status, reply := h.post(t, h.tokens["mallory"], "/api/v1/tasks/"+taskID+"/resolve", map[string]any{}); status != http.StatusOK {
		t.Fatalf("mallory handing it back: %d (%s)", status, strings.TrimSpace(reply))
	}
	if got := h.delegatedBy(t, "alice"); len(got) != 0 {
		t.Fatalf("the task is back with alice and she is still shown %v as delegated", got)
	}
}

// Review focus 4. A pod still on the release before this one writes a
// delegation the old way after migration 32 has run: status delegated, no
// owner, no state. It has nobody to go back to, so it is its assignee's to
// complete, as it was — not a task waiting for a hand-back nobody can make.
func TestADelegationWrittenBeforeTheUpgradeIsStillItsAssigneesToComplete(t *testing.T) {
	h := newTaskHarness(t)
	taskID := h.openTask(t, heldByAlice())
	if err := h.db.Exec(`UPDATE tasks SET status = 'delegated' WHERE id = ?`, taskID).Error; err != nil {
		t.Fatalf("write the delegation the old way: %v", err)
	}

	if got := h.delegatedBy(t, "alice"); len(got) != 0 {
		t.Fatalf("a delegation with no owner is listed for alice as one she made: %v", got)
	}
	if status, reply := h.post(t, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/resolve", map[string]any{}); status != http.StatusBadRequest {
		t.Fatalf("handing back a delegation with no owner: got %d (%s), want 400", status, strings.TrimSpace(reply))
	}
	if status, reply := h.complete(t, "alice", taskID, map[string]any{"approved": true}); status != http.StatusOK {
		t.Fatalf("alice completing a task delegated to her the old way: got %d (%s), want 200", status, strings.TrimSpace(reply))
	}
}

// Review focus 5. The engine withdraws an activity's tasks by writing their
// status alone (cancelOpenTasksOn, through TaskRepository.UpdateStatus) — when
// a boundary event interrupts the step and, since migration 31's release, when
// a repeating approval's rule is met without them. A delegated one keeps its
// owner and its pending state. It is no longer something its owner is waiting
// for.
func TestAWithdrawnDelegationLeavesItsOwnersList(t *testing.T) {
	h := newTaskHarness(t)
	taskID := h.delegatedToMallory(t)
	if err := h.db.Exec(`UPDATE tasks SET status = 'canceled' WHERE id = ?`, taskID).Error; err != nil {
		t.Fatalf("withdraw the task as the engine does: %v", err)
	}
	if got := h.delegatedBy(t, "alice"); len(got) != 0 {
		t.Fatalf("the task was withdrawn and alice is still shown %v as with her delegate", got)
	}
	if status, reply := h.post(t, h.tokens["mallory"], "/api/v1/tasks/"+taskID+"/resolve", map[string]any{}); status != http.StatusBadRequest {
		t.Fatalf("handing back a withdrawn task: got %d (%s), want 400", status, strings.TrimSpace(reply))
	}
	// The pin. Whether a task is closed is asked before whose it is, as it was
	// before delegations had owners: a withdrawn task is told it was withdrawn
	// (a 400), by either of them — not to hand back something that is gone, and
	// not the 403 a pending delegation gets.
	for _, who := range []string{"mallory", "alice"} {
		status, reply := h.complete(t, who, taskID, map[string]any{"approved": true})
		if status != http.StatusBadRequest || !strings.Contains(reply, "withdrawn") || strings.Contains(reply, "hand it back") {
			t.Fatalf("%s completing a withdrawn delegation: got %d (%s); want the 400 a withdrawn task has always been told since migration 31's release",
				who, status, strings.TrimSpace(reply))
		}
	}
}
