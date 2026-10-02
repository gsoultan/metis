package task_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/tests/testutils"
)

// Assign and delegate took any non-empty name. Nothing asked whether it named
// an account, whether that account was in the task's organization, whether it
// was one of the people the step is offered to, or whether separation of
// duties would refuse them the step — which is only found out when they try to
// complete it, with the task already parked on them.

// offeredToFinance is a one-step approval offered to alice and to the finance
// team.
func offeredToFinance() entities.Node {
	return entities.Node{
		Name: "Approve the refund", Type: entities.UserTask,
		CandidateUsers:  []*entities.User{{Username: "alice"}},
		CandidateGroups: []*entities.Group{{Name: "finance"}},
		Properties:      testutils.FormDeclaring("approved"),
	}
}

// putInFinance makes the finance team, once, and puts an account in it.
func (h *taskHarness) putInFinance(t *testing.T, username string) {
	t.Helper()
	tctx := h.tenantContext()
	account, err := h.svc.GetUserByUsername(tctx, username)
	if err != nil {
		t.Fatalf("read %s: %v", username, err)
	}
	finance := entities.Group{ID: uuid.Must(uuid.NewV7()), Name: "finance", Organization: &entities.Organization{ID: h.orgID}}
	if err := h.svc.CreateGroup(tctx, finance); err != nil {
		t.Fatalf("create the finance team: %v", err)
	}
	if err := h.svc.AddMembership(tctx, account.ID, finance.ID); err != nil {
		t.Fatalf("put %s in the finance team: %v", username, err)
	}
}

// openTaskOnNode is the id of the open task on a node of the harness's project.
func (h *taskHarness) openTaskOnNode(t *testing.T, nodeID string) string {
	t.Helper()
	tasks, err := h.svc.ListTasks(h.tenantContext(), h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.NodeID() != nodeID {
			continue
		}
		switch task.Status {
		case entities.TaskUnclaimed, entities.TaskClaimed, entities.TaskDelegated:
			return task.ID.String()
		}
	}
	t.Fatalf("no open task on %q", nodeID)
	return ""
}

func TestATaskCannotBeHandedToSomebodyWhoIsNotThere(t *testing.T) {
	h := newTaskHarness(t)
	boss := h.signInAdministrator(t, "boss")

	// zed has an account — in another organization.
	other, err := h.svc.CreateOrganization(context.Background(), "Other Org", "")
	if err != nil {
		t.Fatalf("create the other organization: %v", err)
	}
	elsewhere := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: other.ID.String()})
	if err := h.svc.CreateUser(elsewhere, entities.User{
		Username: "zed", Roles: []string{entities.RoleUser},
		Organizations: []*entities.Organization{{ID: other.ID}},
	}, "actor-test-password"); err != nil {
		t.Fatalf("create zed: %v", err)
	}

	for _, action := range []string{"assign", "delegate"} {
		t.Run(action, func(t *testing.T) {
			taskID := h.openTask(t, heldByAlice())
			path := "/api/v1/tasks/" + taskID + "/" + action
			replies := map[string]string{}
			for _, target := range []string{"ghost", "zed"} {
				for who, token := range map[string]string{"alice": h.tokens["alice"], "an administrator": boss} {
					status, reply := h.post(t, token, path, map[string]any{"user_id": target, "reason": "they asked for it"})
					if status != http.StatusBadRequest || !strings.Contains(reply, "there is nobody called") {
						t.Fatalf("%s trying to %s the task to %s: got %d (%s); want 400 saying nobody here is called that",
							who, action, target, status, strings.TrimSpace(reply))
					}
					replies[target] = strings.ReplaceAll(reply, target, "NAME")
				}
			}
			// The same words for a name nobody has and a name somebody in
			// another organization has: the reply must not say which.
			if replies["ghost"] != replies["zed"] {
				t.Fatalf("an unknown name is told %q and another organization's member %q; the difference says zed exists",
					replies["ghost"], replies["zed"])
			}
			if got := h.taskAssignee(t, taskID); got != "alice" {
				t.Fatalf("after the refusals the task is held by %q, want alice", got)
			}
			// Handing it to whoever already has it is not a hand-over.
			if status, reply := h.post(t, boss, path, map[string]any{"user_id": "alice", "reason": "again"}); status != http.StatusBadRequest || !strings.Contains(reply, "already holds this task") {
				t.Fatalf("trying to %s the task to its holder: got %d (%s), want 400", action, status, strings.TrimSpace(reply))
			}
		})
	}
}

// D9: separation of duties is not something a hand-over can be talked past.
func TestATaskCannotBeHandedToSomebodySeparationOfDutiesForbids(t *testing.T) {
	h := newTaskHarness(t)
	boss := h.signInAdministrator(t, "boss")
	h.tokens["ada"] = h.signInWithRoles(t, "ada", entities.RoleUser)
	h.tokens["bo"] = h.signInWithRoles(t, "bo", entities.RoleUser)

	tctx := h.tenantContext()
	if _, err := h.svc.CreateDefinition(tctx, fourEyes(h.projID, true)); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if _, err := h.svc.StartProcess(tctx, h.projID, "four-eyes", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	if status, reply := h.post(t, h.tokens["ada"], "/api/v1/tasks/"+h.openTaskOnNode(t, "submit")+"/complete", map[string]any{}); status != http.StatusOK {
		t.Fatalf("ada submitting: %d (%s)", status, reply)
	}
	approve := h.openTaskOnNode(t, "approve")
	if status, reply := h.post(t, h.tokens["bo"], "/api/v1/tasks/"+approve+"/claim", map[string]any{}); status != http.StatusOK {
		t.Fatalf("bo claiming the approval: %d (%s)", status, reply)
	}

	for who, token := range map[string]string{"bo, who holds it": h.tokens["bo"], "an administrator with a reason": boss} {
		for _, action := range []string{"assign", "delegate"} {
			status, reply := h.post(t, token, "/api/v1/tasks/"+approve+"/"+action,
				map[string]any{"user_id": "ada", "reason": "ada knows this customer"})
			// The step's name is quoted in the reply, and the reply is JSON, so
			// the test looks for the words either side of it.
			if status != http.StatusBadRequest || !strings.Contains(reply, "ada already did") || !strings.Contains(reply, "the same person may not also do") {
				t.Fatalf("%s trying to %s the approval to ada, who submitted it: got %d (%s); want 400 saying the same person may not do both",
					who, action, status, strings.TrimSpace(reply))
			}
		}
	}
	if got := h.taskAssignee(t, approve); got != "bo" {
		t.Fatalf("after the refusals the approval is held by %q, want bo", got)
	}
}

func TestATaskOfferedToPeopleIsHandedOnlyToOneOfThem(t *testing.T) {
	h := newTaskHarness(t)
	boss := h.signInAdministrator(t, "boss")
	h.tokens["gina"] = h.signInWithRoles(t, "gina", entities.RoleUser)
	h.putInFinance(t, "gina")

	claimed := func() (string, uuid.UUID) {
		taskID, instanceID := h.openTaskWith(t, offeredToFinance(), nil)
		if status, reply := h.post(t, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/claim", map[string]any{}); status != http.StatusOK {
			t.Fatalf("alice claiming: %d (%s)", status, reply)
		}
		return taskID, instanceID
	}

	// A member of a team it is offered to is one of them.
	taskID, _ := claimed()
	if status, reply := h.post(t, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/assign", map[string]any{"user_id": "gina"}); status != http.StatusOK {
		t.Fatalf("alice handing the task to gina, who is in finance: got %d (%s), want 200", status, strings.TrimSpace(reply))
	}

	// Somebody it is not offered to is not, whoever holds it asks.
	taskID, instanceID := claimed()
	for _, action := range []string{"assign", "delegate"} {
		status, reply := h.post(t, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/"+action, map[string]any{"user_id": "mallory", "reason": "she offered"})
		if status != http.StatusBadRequest || !strings.Contains(reply, "only to one of the people or teams it is offered to") {
			t.Fatalf("alice trying to %s the task to mallory, who is not offered it: got %d (%s), want 400", action, status, strings.TrimSpace(reply))
		}
	}
	if got := h.taskAssignee(t, taskID); got != "alice" {
		t.Fatalf("after the refusals the task is held by %q, want alice", got)
	}

	// An administrator may send it there, saying why, and the trail says it
	// went to somebody it was not offered to.
	if status, reply := h.post(t, boss, "/api/v1/tasks/"+taskID+"/assign", map[string]any{"user_id": "mallory", "reason": "nobody in finance is in this week"}); status != http.StatusOK {
		t.Fatalf("an administrator handing the task to mallory with a reason: got %d (%s), want 200", status, strings.TrimSpace(reply))
	}
	entry := h.lastOf(t, instanceID, "task_assigned")
	if entry.Data["candidate_override"] != true {
		t.Errorf("the entry does not record that the task went to somebody it was not offered to: %v", entry.Data)
	}
	if want := `boss reassigned task "Approve the refund" from alice to mallory, who is not one of the people it is offered to: nobody in finance is in this week`; entry.Narrative != want {
		t.Errorf("the entry reads %q, want %q", entry.Narrative, want)
	}

	// An administrator who holds the task is not asked why they hand it on —
	// until it goes to somebody it is not offered to.
	taskID, _ = claimed()
	if status, reply := h.post(t, boss, "/api/v1/tasks/"+taskID+"/assign", map[string]any{"user_id": "boss", "reason": "taking it while alice is away"}); status != http.StatusOK {
		t.Fatalf("an administrator taking the task: got %d (%s)", status, strings.TrimSpace(reply))
	}
	status, reply := h.post(t, boss, "/api/v1/tasks/"+taskID+"/assign", map[string]any{"user_id": "mallory"})
	if status != http.StatusBadRequest || !strings.Contains(reply, "say why it goes to them anyway") {
		t.Fatalf("an administrator holding the task and handing it to mallory with no reason: got %d (%s), want 400 asking why", status, strings.TrimSpace(reply))
	}
	if status, reply := h.post(t, boss, "/api/v1/tasks/"+taskID+"/assign", map[string]any{"user_id": "gina"}); status != http.StatusOK {
		t.Fatalf("an administrator holding the task and handing it to gina, who is offered it, with no reason: got %d (%s), want 200", status, strings.TrimSpace(reply))
	}
}

// The pin. Claiming asks the same two questions through the code this task
// takes apart, and is told what it always was: forbidden, a 403 — not the 400 a
// hand-over to the same person gets.
func TestAClaimSeparationOfDutiesForbidsIsStillForbidden(t *testing.T) {
	h := newTaskHarness(t)
	h.tokens["ada"] = h.signInWithRoles(t, "ada", entities.RoleUser)

	tctx := h.tenantContext()
	if _, err := h.svc.CreateDefinition(tctx, fourEyes(h.projID, true)); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if _, err := h.svc.StartProcess(tctx, h.projID, "four-eyes", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	if status, reply := h.post(t, h.tokens["ada"], "/api/v1/tasks/"+h.openTaskOnNode(t, "submit")+"/complete", map[string]any{}); status != http.StatusOK {
		t.Fatalf("ada submitting: %d (%s)", status, reply)
	}
	status, reply := h.post(t, h.tokens["ada"], "/api/v1/tasks/"+h.openTaskOnNode(t, "approve")+"/claim", map[string]any{})
	if status != http.StatusForbidden || !strings.Contains(reply, "may not be done by the same person") {
		t.Fatalf("ada claiming the approval of what she submitted: got %d (%s); want the 403 it has always been", status, strings.TrimSpace(reply))
	}
}
