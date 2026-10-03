package task_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// No task request carried a reason. An administrator could take an approval
// off the person it was given to and hand it to somebody else, and nothing
// asked why. A reason is required now from anyone but the task's holder
// (D8), and it is the service that asks, holding the task's row.

func TestHandingOnATaskThatIsNotYoursNeedsAReason(t *testing.T) {
	h := newTaskHarness(t)
	boss := h.signInAdministrator(t, "boss")
	h.tokens["bob"] = h.signInWithRoles(t, "bob", entities.RoleUser)

	for _, action := range []string{"assign", "delegate"} {
		t.Run(action, func(t *testing.T) {
			taskID := h.openTask(t, heldByAlice())
			path := "/api/v1/tasks/" + taskID + "/" + action

			for name, body := range map[string]map[string]any{
				"no reason":          {"user_id": "bob"},
				"a reason of spaces": {"user_id": "bob", "reason": "   "},
			} {
				status, reply := h.post(t, boss, path, body)
				if status != http.StatusBadRequest || !strings.Contains(reply, "say why") {
					t.Fatalf("an administrator trying to %s alice's task with %s: got %d (%s); want 400 asking why",
						action, name, status, strings.TrimSpace(reply))
				}
				if got := h.taskAssignee(t, taskID); got != "alice" {
					t.Fatalf("after the refused %s the task is held by %q, want alice", action, got)
				}
			}

			status, reply := h.post(t, boss, path, map[string]any{"user_id": "bob", "reason": "alice is on leave until Monday"})
			if status != http.StatusOK {
				t.Fatalf("an administrator trying to %s alice's task with a reason: got %d (%s)", action, status, strings.TrimSpace(reply))
			}
			if got := h.taskAssignee(t, taskID); got != "bob" {
				t.Fatalf("after the administrator's %s the task is held by %q, want bob", action, got)
			}
		})
	}
}

func TestTheHolderHandsTheirOwnTaskOnWithoutAReason(t *testing.T) {
	h := newTaskHarness(t)
	for _, action := range []string{"assign", "delegate"} {
		taskID := h.openTask(t, heldByAlice())
		status, reply := h.post(t, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/"+action, map[string]any{"user_id": "mallory"})
		if status != http.StatusOK {
			t.Fatalf("alice trying to %s her own task with no reason: got %d (%s), want 200", action, status, strings.TrimSpace(reply))
		}
	}
	taskID := h.openTask(t, heldByAlice())
	if status, reply := h.raw(t, http.MethodPost, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/unclaim", ""); status != http.StatusOK {
		t.Fatalf("alice releasing her own task with no body at all: got %d (%s), want 200", status, strings.TrimSpace(reply))
	}
}

func TestReleasingSomebodyElsesTaskNeedsAReason(t *testing.T) {
	h := newTaskHarness(t)
	boss := h.signInAdministrator(t, "boss")
	taskID := h.openTask(t, heldByAlice())
	path := "/api/v1/tasks/" + taskID + "/unclaim"

	status, reply := h.post(t, boss, path, map[string]any{})
	if status != http.StatusBadRequest || !strings.Contains(reply, "say why") {
		t.Fatalf("an administrator releasing alice's task with no reason: got %d (%s); want 400 asking why", status, strings.TrimSpace(reply))
	}
	if got := h.taskStatus(t, taskID); got != string(entities.TaskClaimed) {
		t.Fatalf("after the refused release the task is %q, want it still claimed", got)
	}
	if status, reply := h.post(t, boss, path, map[string]any{"reason": "alice left the company"}); status != http.StatusOK {
		t.Fatalf("an administrator releasing alice's task with a reason: got %d (%s)", status, strings.TrimSpace(reply))
	}
	if got := h.taskStatus(t, taskID); got != string(entities.TaskUnclaimed) {
		t.Fatalf("after the release the task is %q, want unclaimed", got)
	}

	// Nobody holds it now, so there is nothing to release — the caller's
	// mistake, not the server's.
	if status, reply := h.post(t, boss, path, map[string]any{"reason": "again"}); status != http.StatusBadRequest {
		t.Fatalf("releasing a task nobody holds: got %d (%s), want 400", status, strings.TrimSpace(reply))
	}
}

// The denial: somebody who neither holds the task nor administers the
// organization is refused, and a reason does not change that.
func TestAReasonDoesNotLetAStrangerHandATaskOn(t *testing.T) {
	h := newTaskHarness(t)
	h.tokens["olga"] = h.signInWithRoles(t, "olga", entities.RoleOperator)
	taskID := h.openTask(t, heldByAlice())

	for _, who := range []string{"mallory", "olga"} {
		for _, action := range []string{"assign", "delegate", "unclaim"} {
			status, reply := h.post(t, h.tokens[who], "/api/v1/tasks/"+taskID+"/"+action,
				map[string]any{"user_id": who, "reason": "I would like it"})
			if status != http.StatusForbidden {
				t.Fatalf("%s, who neither holds the task nor administers the organization, trying to %s it: got %d (%s), want 403",
					who, action, status, strings.TrimSpace(reply))
			}
		}
	}
	if got, status := h.taskAssignee(t, taskID), h.taskStatus(t, taskID); got != "alice" || status != string(entities.TaskClaimed) {
		t.Fatalf("after the refusals the task is %s and held by %q, want claimed by alice", status, got)
	}
	// And naming nobody tells a stranger nothing more than that they may not.
	if status, reply := h.post(t, h.tokens["mallory"], "/api/v1/tasks/"+taskID+"/assign", map[string]any{}); status != http.StatusForbidden {
		t.Fatalf("mallory assigning alice's task to nobody: got %d (%s), want 403 before anything else is said", status, strings.TrimSpace(reply))
	}
}

func TestAReasonIsBounded(t *testing.T) {
	h := newTaskHarness(t)
	taskID := h.openTask(t, heldByAlice())
	status, reply := h.post(t, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/assign",
		map[string]any{"user_id": "mallory", "reason": strings.Repeat("x", 1001)})
	if status != http.StatusBadRequest || !strings.Contains(reply, "longer than 1000 characters") {
		t.Fatalf("a reason of 1001 characters: got %d (%s), want 400 saying it is too long", status, strings.TrimSpace(reply))
	}
	if got := h.taskAssignee(t, taskID); got != "alice" {
		t.Fatalf("after the refusal the task is held by %q, want alice", got)
	}
}

// Review focus 1. A body the server cannot read used to reach the encoder as
// a plain error and was answered 500, which spends the 5xx budget on a
// client's typo.
func TestAHandOverBodyTheServerCannotReadIsTheCallersMistake(t *testing.T) {
	h := newTaskHarness(t)
	taskID := h.openTask(t, heldByAlice())
	for _, action := range []string{"assign", "delegate", "unclaim"} {
		status, reply := h.raw(t, http.MethodPost, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/"+action, `{"user_id": "mallory"`)
		if status != http.StatusBadRequest || !strings.Contains(reply, "not JSON the server can read") {
			t.Fatalf("a truncated body sent to %s: got %d (%s), want 400 saying the body could not be read", action, status, strings.TrimSpace(reply))
		}
	}
	// No body at all names nobody, and is told so.
	status, reply := h.raw(t, http.MethodPost, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/assign", "")
	if status != http.StatusBadRequest || !strings.Contains(reply, "say who the task is assigned to") {
		t.Fatalf("an assign with no body: got %d (%s), want 400 asking who", status, strings.TrimSpace(reply))
	}
	if got := h.taskAssignee(t, taskID); got != "alice" {
		t.Fatalf("after the refusals the task is held by %q, want alice", got)
	}
}

// The 400 for a body the server cannot read carried the decoder's own words:
// `{"user_id": 5}` was answered "json: cannot unmarshal number into Go struct
// field DelegateTaskRequest.user_id of type string". That is the server's
// source read out to whoever sent the request. The answer is a sentence.
func TestAHandOverBodyOfTheWrongKindIsRefusedWithoutNamingTheServersTypes(t *testing.T) {
	h := newTaskHarness(t)
	taskID := h.openTask(t, heldByAlice())
	path := "/api/v1/tasks/" + taskID
	requests := []struct{ method, path, body string }{
		{http.MethodPost, path + "/assign", `{"user_id": 5}`},
		{http.MethodPost, path + "/delegate", `{"user_id": 5}`},
		{http.MethodPost, path + "/unclaim", `{"reason": 5}`},
		{http.MethodPost, path + "/resolve", `{"reason": ["because"]}`},
		{http.MethodPut, path, `{"priority": "high"}`},
		{http.MethodPost, path + "/assign", `{"user_id": "mallory"`},
	}
	for _, request := range requests {
		status, reply := h.raw(t, request.method, h.tokens["alice"], request.path, request.body)
		reply = strings.TrimSpace(reply)
		if status != http.StatusBadRequest || !strings.Contains(reply, "not JSON the server can read") {
			t.Errorf("%s %s with %s: got %d (%s), want 400 saying the body could not be read",
				request.method, request.path, request.body, status, reply)
		}
		for _, leaked := range []string{"json:", "unmarshal", "Go struct", "Request", "of type", "unexpected EOF"} {
			if strings.Contains(reply, leaked) {
				t.Errorf("%s %s with %s was answered %q, which repeats the decoder's %q",
					request.method, request.path, request.body, reply, leaked)
			}
		}
	}
	if got := h.taskAssignee(t, taskID); got != "alice" {
		t.Fatalf("after the refusals the task is held by %q, want alice", got)
	}
}
