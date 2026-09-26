package task_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// A task nobody was named for — no assignee, no candidate users, no candidate
// groups — was everybody's. The candidate check read the absence of a
// constraint as "anyone", so somebody in accounts payable could pick up and
// complete an approval nobody had ever meant them to have. Absent constraint
// means deny: such a task is the administrators' and the operators'.

// nobodyNamed is a step with no assignee and no candidates.
func nobodyNamed() entities.Node {
	return entities.Node{Name: "Approve the refund", Type: entities.UserTask}
}

// legacyUnassignedClaims is the setting that brings the old rule back.
const legacyUnassignedClaims = "METIS_ALLOW_UNASSIGNED_TASK_CLAIMS"

func TestATaskNobodyWasNamedForIsClaimedOnlyByAnAdministratorOrAnOperator(t *testing.T) {
	h := newTaskHarness(t)
	h.tokens["olga"] = h.signInWithRoles(t, "olga", entities.RoleOperator)
	h.tokens["ada"] = h.signInWithRoles(t, "ada", entities.RoleAdmin)

	taskID := h.openTask(t, nobodyNamed())
	status, body := h.post(t, h.tokens["mallory"], "/api/v1/tasks/"+taskID+"/claim", map[string]any{})
	if status != http.StatusForbidden || !saysWhoMayTakeIt(body) {
		t.Fatalf("mallory, a member with no role, claiming a task nobody was named for: got %d (%s); "+
			"want 403 saying it has no assignee and no candidates and that an administrator or an operator can take it",
			status, strings.TrimSpace(body))
	}
	if got := h.taskStatus(t, taskID); got != string(entities.TaskUnclaimed) {
		t.Fatalf("after mallory was refused the task is %q, want it still unclaimed", got)
	}

	for _, who := range []string{"olga", "ada"} {
		taskID := h.openTask(t, nobodyNamed())
		if status, body := h.post(t, h.tokens[who], "/api/v1/tasks/"+taskID+"/claim", map[string]any{}); status != http.StatusOK {
			t.Fatalf("%s claiming a task nobody was named for: got %d (%s), want 200", who, status, body)
		}
		if got := h.taskStatus(t, taskID); got != string(entities.TaskClaimed) {
			t.Fatalf("after %s claimed it the task is %q, want claimed", who, got)
		}
	}
}

func TestATaskNobodyWasNamedForIsCompletedOnlyByAnAdministratorOrAnOperator(t *testing.T) {
	h := newTaskHarness(t)
	h.tokens["olga"] = h.signInWithRoles(t, "olga", entities.RoleOperator)
	h.tokens["ada"] = h.signInWithRoles(t, "ada", entities.RoleAdmin)
	complete := func(who, taskID string) (int, string) {
		return h.post(t, h.tokens[who], "/api/v1/tasks/"+taskID+"/complete",
			map[string]any{"variables": map[string]any{"approved": true}})
	}

	taskID := h.openTask(t, nobodyNamed())
	if status, body := complete("mallory", taskID); status != http.StatusForbidden || !saysWhoMayTakeIt(body) {
		t.Fatalf("mallory, a member with no role, completing a task nobody was named for: got %d (%s); "+
			"want 403 saying it has no assignee and no candidates and that an administrator or an operator can take it",
			status, strings.TrimSpace(body))
	}
	if got := h.taskStatus(t, taskID); got != string(entities.TaskUnclaimed) {
		t.Fatalf("after mallory was refused the task is %q, want it still open", got)
	}

	for _, who := range []string{"olga", "ada"} {
		taskID := h.openTask(t, nobodyNamed())
		if status, body := complete(who, taskID); status != http.StatusOK {
			t.Fatalf("%s completing a task nobody was named for: got %d (%s), want 200", who, status, body)
		}
		if got := h.taskStatus(t, taskID); got != string(entities.TaskCompleted) {
			t.Fatalf("after %s completed it the task is %q, want completed", who, got)
		}
	}
}

// An installation whose processes rely on the old rule can have it back for a
// migration window. With the setting on, anybody signed in to the organization
// may claim and complete such a task, as before.
func TestTheLegacySettingLetsAnybodyTakeATaskNobodyWasNamedFor(t *testing.T) {
	t.Setenv(legacyUnassignedClaims, "true")
	h := newTaskHarness(t)

	claimed := h.openTask(t, nobodyNamed())
	if status, body := h.post(t, h.tokens["mallory"], "/api/v1/tasks/"+claimed+"/claim", map[string]any{}); status != http.StatusOK {
		t.Fatalf("with %s on, mallory claiming a task nobody was named for: got %d (%s), want 200",
			legacyUnassignedClaims, status, body)
	}

	completed := h.openTask(t, nobodyNamed())
	if status, body := h.post(t, h.tokens["mallory"], "/api/v1/tasks/"+completed+"/complete",
		map[string]any{"variables": map[string]any{"approved": true}}); status != http.StatusOK {
		t.Fatalf("with %s on, mallory completing a task nobody was named for: got %d (%s), want 200",
			legacyUnassignedClaims, status, body)
	}
	if got := h.taskStatus(t, completed); got != string(entities.TaskCompleted) {
		t.Fatalf("with %s on, the task mallory completed is %q, want completed", legacyUnassignedClaims, got)
	}
}

// A manual task is the one kind left open to anybody in its organization. The
// designer has no field to name anybody for one, and tells its author that an
// empty one is for anybody to pick up; what it asks is that a person confirm
// they did something away from the system.
func TestAManualTaskNobodyWasNamedForIsStillAnybodys(t *testing.T) {
	h := newTaskHarness(t)

	taskID := h.openTask(t, entities.Node{Name: "Ship the parcel", Type: entities.ManualTask})
	if status, body := h.post(t, h.tokens["mallory"], "/api/v1/tasks/"+taskID+"/claim", map[string]any{}); status != http.StatusOK {
		t.Fatalf("mallory claiming a manual task nobody was named for: got %d (%s), want 200", status, body)
	}
	if status, body := h.post(t, h.tokens["mallory"], "/api/v1/tasks/"+taskID+"/complete", map[string]any{}); status != http.StatusOK {
		t.Fatalf("mallory confirming a manual task she claimed: got %d (%s), want 200", status, body)
	}
}

// The roles that let somebody take such a task are the signed-in caller's, and
// they count only for the caller acting as themselves. A call that names
// somebody else as the actor is refused rather than lent the caller's roles.
func TestAnOperatorsRolesAreNotLentToSomebodyElse(t *testing.T) {
	repo, svc, ctx, projectID := newTaskService(t)
	taskID := seedTask(t, repo, ctx, projectID, entities.Task{
		Name:   "Approve the refund",
		Type:   entities.UserTask,
		Status: entities.TaskUnclaimed,
		Node:   &entities.Node{ID: "approve"},
	})
	asOlga := context.WithValue(ctx, pkgauth.UserContextKey,
		entities.User{Username: "olga", Roles: []string{entities.RoleOperator}})

	if err := svc.ClaimTask(asOlga, taskID, "mallory"); !errors.Is(err, apierr.ErrForbidden) {
		t.Fatalf("an operator's request naming mallory as the one claiming: got %v, want a refusal", err)
	}
	if err := svc.ClaimTask(asOlga, taskID, "olga"); err != nil {
		t.Fatalf("the operator claiming it herself: %v", err)
	}
}

// saysWhoMayTakeIt reports whether a refusal says why, and who can.
func saysWhoMayTakeIt(body string) bool {
	return strings.Contains(body, "no assignee and no candidates") &&
		strings.Contains(body, "an administrator or an operator")
}

// signInWithRoles creates an account in the harness's organization holding the
// roles, and signs it in.
func (h *taskHarness) signInWithRoles(t *testing.T, name string, roles ...string) string {
	t.Helper()
	tctx := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: h.orgID.String()})
	if err := h.svc.CreateUser(tctx, entities.User{
		Username:      name,
		Roles:         roles,
		Organizations: []*entities.Organization{{ID: h.orgID}},
	}, "actor-test-password"); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return h.login(t, name)
}

// openTask deploys a process whose one step is the one given, starts it, and
// returns the id of the task that step opened.
func (h *taskHarness) openTask(t *testing.T, step entities.Node) string {
	t.Helper()
	ctx := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: h.orgID.String()})
	step.ID, step.Incoming, step.Outgoing = "step", []string{"f1"}, []string{"f2"}
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "one-step",
		Name:    "One step",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			&step,
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "step"},
			{ID: "f2", SourceRef: "step", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, def); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "one-step", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	page, err := h.svc.ListTasksByInstancePaged(ctx, instanceID, repocontracts.Pagination{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list the instance's tasks: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("the instance opened %d tasks, want 1", len(page.Items))
	}
	return page.Items[0].ID.String()
}
