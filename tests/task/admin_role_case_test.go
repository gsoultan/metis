package task_test

import (
	"net/http"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// An administrator whose role is stored as "admin" — what the Platform access
// page wrote until 2026-09-13, and what the API still accepts — passed every
// administrator-only endpoint, whose role check ignores case. Releasing,
// delegating or assigning a task somebody else holds then refused them as not
// an administrator, because that check compared exactly.
func TestAnAdministratorWhoseRoleIsLowercaseMayReleaseSomebodysTask(t *testing.T) {
	h := newTaskHarness(t)
	taskID := h.assignTaskTo(t, "alice")

	tenant := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: h.orgID.String()})
	if err := h.svc.CreateUser(tenant, entities.User{
		Username:      "olga",
		Roles:         []string{"admin"},
		Organizations: []*entities.Organization{{ID: h.orgID}},
	}, "actor-test-password"); err != nil {
		t.Fatalf("create olga: %v", err)
	}
	token := h.login(t, "olga")

	status, body := h.post(t, token, "/api/v1/tasks/"+taskID+"/unclaim", map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("an administrator whose role reads \"admin\" releasing alice's task: got %d (%s), want 200", status, body)
	}
	if got := h.taskStatus(t, taskID); got != string(entities.TaskUnclaimed) {
		t.Fatalf("after the administrator released it the task is %q, want unclaimed", got)
	}
}
