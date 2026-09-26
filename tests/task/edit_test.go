package task_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Editing a task — its name, priority or due date — asked nobody's
// permission. Any signed-in member of the organization could rename a task
// somebody else was holding, push its due date out or drop its priority, which
// is how a piece of work slides down its holder's inbox and past its deadline
// without them doing anything. Releasing and handing a task on already needed
// the holder or an administrator; editing it now does too.
func TestOnlyTheHolderOrAnAdministratorMayEditATask(t *testing.T) {
	h := newTaskHarness(t)
	taskID := h.assignTaskTo(t, "alice")
	edit := func(token, name string, priority int) (int, string) {
		return h.do(t, http.MethodPut, token, "/api/v1/tasks/"+taskID, map[string]any{"name": name, "priority": priority})
	}

	status, body := edit(h.tokens["mallory"], "Ignore this", 1)
	if name, priority := h.taskNameAndPriority(t, taskID); status != http.StatusForbidden || name != "Approve" || priority != 0 {
		t.Fatalf("mallory editing alice's task: got %d (%s) and the task is now %q at priority %d; "+
			"want 403 and the task as it was", status, body, name, priority)
	}

	if status, body := edit(h.tokens["alice"], "Approve the refund", 50); status != http.StatusOK {
		t.Fatalf("alice editing her own task: got %d (%s)", status, body)
	}
	if name, priority := h.taskNameAndPriority(t, taskID); name != "Approve the refund" || priority != 50 {
		t.Fatalf("after alice's edit the task is %q at priority %d", name, priority)
	}

	administrator := h.signInAdministrator(t, "boss")
	if status, body := edit(administrator, "Approve the refund today", 90); status != http.StatusOK {
		t.Fatalf("an administrator editing somebody else's task: got %d (%s)", status, body)
	}
	if name, priority := h.taskNameAndPriority(t, taskID); name != "Approve the refund today" || priority != 90 {
		t.Fatalf("after the administrator's edit the task is %q at priority %d", name, priority)
	}
}

// signInAdministrator creates an administrator of the harness's organization.
func (h *taskHarness) signInAdministrator(t *testing.T, name string) string {
	t.Helper()
	tctx := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: h.orgID.String()})
	if err := h.svc.CreateUser(tctx, entities.User{
		Username:      name,
		Roles:         []string{entities.RoleAdmin},
		Organizations: []*entities.Organization{{ID: h.orgID}},
	}, "actor-test-password"); err != nil {
		t.Fatalf("create administrator %s: %v", name, err)
	}
	return h.login(t, name)
}

func (h *taskHarness) taskNameAndPriority(t *testing.T, id string) (string, int) {
	t.Helper()
	status, body := h.get(t, h.tokens["alice"], "/api/v1/tasks/"+id)
	if status != http.StatusOK {
		t.Fatalf("get task: status %d (%s)", status, body)
	}
	var out struct {
		Task struct {
			Name     string `json:"name"`
			Priority int    `json:"priority"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return out.Task.Name, out.Task.Priority
}
