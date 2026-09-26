package task_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
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

// Handing a task on — delegating it, or assigning it — was its holder's or an
// administrator's. A task nobody was named for has no holder, so an operator,
// whose work such a task now is, could claim it and could not give it to the
// person it should have gone to, and a member was told only that the holder
// could. Giving it to somebody is taking it on their behalf, so the rule that
// decides claiming decides this too.
func TestATaskNobodyWasNamedForIsHandedOnOnlyByAnAdministratorOrAnOperator(t *testing.T) {
	h := newTaskHarness(t)
	h.tokens["olga"] = h.signInWithRoles(t, "olga", entities.RoleOperator)
	h.tokens["ada"] = h.signInWithRoles(t, "ada", entities.RoleAdmin)

	for _, action := range []string{"delegate", "assign"} {
		taskID := h.openTask(t, nobodyNamed())
		status, body := h.post(t, h.tokens["mallory"], "/api/v1/tasks/"+taskID+"/"+action, map[string]any{"user_id": "mallory"})
		if status != http.StatusForbidden || !saysWhoMayTakeIt(body) {
			t.Fatalf("mallory, a member with no role, trying to %s a task nobody was named for: got %d (%s); "+
				"want 403 saying it has no assignee and no candidates and that an administrator or an operator can take it",
				action, status, strings.TrimSpace(body))
		}
		if got := h.taskAssignee(t, taskID); got != "" {
			t.Fatalf("after mallory was refused the task is held by %q, want nobody", got)
		}

		for _, who := range []string{"olga", "ada"} {
			taskID := h.openTask(t, nobodyNamed())
			if status, body := h.post(t, h.tokens[who], "/api/v1/tasks/"+taskID+"/"+action, map[string]any{"user_id": "alice"}); status != http.StatusOK {
				t.Fatalf("%s trying to %s a task nobody was named for to alice: got %d (%s), want 200",
					who, action, status, strings.TrimSpace(body))
			}
			if got := h.taskAssignee(t, taskID); got != "alice" {
				t.Fatalf("after %s gave it to alice (%s) the task is held by %q", who, action, got)
			}
			if status, body := h.post(t, h.tokens["alice"], "/api/v1/tasks/"+taskID+"/complete",
				map[string]any{"variables": map[string]any{"approved": true}}); status != http.StatusOK {
				t.Fatalf("alice completing the task %s gave her (%s): got %d (%s)", who, action, status, strings.TrimSpace(body))
			}
		}
	}

	// Work somebody was named for is still its holder's or an administrator's
	// to give away: an operator cannot hand on a task offered to a team.
	offered := h.openTask(t, entities.Node{
		Name: "Approve the refund", Type: entities.UserTask,
		CandidateGroups: []*entities.Group{{Name: "finance"}},
	})
	if status, body := h.post(t, h.tokens["olga"], "/api/v1/tasks/"+offered+"/delegate", map[string]any{"user_id": "alice"}); status != http.StatusForbidden {
		t.Fatalf("an operator delegating a task offered to finance: got %d (%s), want 403", status, strings.TrimSpace(body))
	}
}

// The inbox's "Available to Claim" is the work its reader may claim. A task
// nobody was named for was listed for nobody — not even for the
// administrators and operators who are now the only people who may take it —
// so the work that fell to them would wait where none of them looked. It is
// listed for them, and still for nobody else.
func TestTheInboxOffersATaskNobodyWasNamedForOnlyToThoseWhoMayTakeIt(t *testing.T) {
	h := newTaskHarness(t)
	h.tokens["olga"] = h.signInWithRoles(t, "olga", entities.RoleOperator)
	h.tokens["ada"] = h.signInWithRoles(t, "ada", entities.RoleAdmin)
	unnamed := h.openTask(t, nobodyNamed())
	offered := h.openTask(t, entities.Node{
		Name: "Check the invoice", Type: entities.UserTask,
		CandidateUsers: []*entities.User{{Username: "mallory"}},
	})
	// A row written some other way than by this server can hold its empty
	// lists as NULL or as the JSON null rather than []. It names nobody all
	// the same.
	stored := h.openTask(t, nobodyNamed())
	if err := h.db.Exec(`UPDATE tasks SET candidate_users = NULL, candidate_groups = 'null' WHERE id = ?`, stored).Error; err != nil {
		t.Fatalf("store the candidate lists as NULL: %v", err)
	}

	mallorys := h.availableToClaim(t, "mallory")
	if !slices.Contains(mallorys, offered) || slices.Contains(mallorys, unnamed) || slices.Contains(mallorys, stored) {
		t.Fatalf("mallory, a member with no role, is offered %v; want the task offered to her (%s) and neither task "+
			"nobody was named for (%s, %s)", mallorys, offered, unnamed, stored)
	}
	for _, who := range []string{"olga", "ada"} {
		if theirs := h.availableToClaim(t, who); !slices.Contains(theirs, unnamed) || !slices.Contains(theirs, stored) {
			t.Fatalf("%s may take a task nobody was named for and is offered %v; want %s and %s among them",
				who, theirs, unnamed, stored)
		}
	}
}

// With the old rule back, anybody may take such a task, so anybody is offered
// it.
func TestWithTheLegacySettingTheInboxOffersATaskNobodyWasNamedForToAnybody(t *testing.T) {
	t.Setenv(legacyUnassignedClaims, "true")
	h := newTaskHarness(t)
	unnamed := h.openTask(t, nobodyNamed())

	if mallorys := h.availableToClaim(t, "mallory"); !slices.Contains(mallorys, unnamed) {
		t.Fatalf("with %s on, mallory is offered %v; want the task nobody was named for (%s) among them",
			legacyUnassignedClaims, mallorys, unnamed)
	}
}

// availableToClaim lists the ids of the tasks the inbox offers somebody to
// claim.
func (h *taskHarness) availableToClaim(t *testing.T, who string) []string {
	t.Helper()
	status, body := h.post(t, h.tokens[who], "/api/v1/tasks/candidates", map[string]any{"page": 1, "page_size": 50})
	if status != http.StatusOK {
		t.Fatalf("list %s's available tasks: status %d (%s)", who, status, body)
	}
	var out struct {
		Tasks []struct {
			ID string `json:"id"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode %s's available tasks: %v", who, err)
	}
	ids := make([]string, 0, len(out.Tasks))
	for _, task := range out.Tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

// taskAssignee reads back who holds a task, or "" when nobody does.
func (h *taskHarness) taskAssignee(t *testing.T, id string) string {
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
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return out.Task.Assignee.Username
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
