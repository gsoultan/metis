package task_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/tests/testutils"
)

// staleMark leaves alice's claimed task the way a pod on the release before
// this one leaves a task this release delegated, once it claims or releases
// it: an owner and a pending mark, under a status that is not "delegated".
func (h *taskHarness) staleMark(t *testing.T) string {
	t.Helper()
	taskID := h.openTask(t, heldByAlice())
	if err := h.db.Exec(`UPDATE tasks SET owner = 'mallory', delegation_state = 'pending' WHERE id = ?`, taskID).Error; err != nil {
		t.Fatalf("leave the row as an old pod would: %v", err)
	}
	return taskID
}

// A task is waiting to be handed back only while it is delegated, has an owner
// and is marked pending — all three. An owner and a pending mark on a task
// that is merely claimed are left over, and the next hand-over or edit takes
// them off rather than carrying them on.
func TestAStalePendingMarkIsTakenOffByTheNextHandOverOrEdit(t *testing.T) {
	h := newTaskHarness(t)

	for _, change := range []struct {
		name, method, action string
		body                 map[string]any
		assignee, owner      string
		state                string
	}{
		{"an edit", http.MethodPut, "", map[string]any{"priority": 70}, "alice", "", ""},
		{"an assignment", http.MethodPost, "/assign", map[string]any{"user_id": "mallory"}, "mallory", "", ""},
		{"a release", http.MethodPost, "/unclaim", map[string]any{}, "", "", ""},
		// A delegation is a real one from here: alice, who holds it, is its
		// owner — not whoever the stale mark named.
		{"a delegation", http.MethodPost, "/delegate", map[string]any{"user_id": "mallory"}, "mallory", "alice", "pending"},
	} {
		t.Run(change.name, func(t *testing.T) {
			taskID := h.staleMark(t)
			if got := h.delegatedBy(t, "mallory"); slices.Contains(got, taskID) {
				t.Fatalf("a task alice holds is listed as with mallory's delegate: %v", got)
			}
			// There is nothing to hand back, whoever asks: alice, who holds
			// it, is told so, and mallory — the owner the leftover mark names,
			// who holds nothing — is told it is not hers to hand back.
			for who, want := range map[string]int{"alice": http.StatusBadRequest, "mallory": http.StatusForbidden} {
				if status, reply := h.post(t, h.tokens[who], "/api/v1/tasks/"+taskID+"/resolve", map[string]any{"reason": "it is mine"}); status != want {
					t.Fatalf("%s handing back a task that is not delegated: got %d (%s), want %d", who, status, strings.TrimSpace(reply), want)
				}
			}
			if status, reply := h.do(t, change.method, h.tokens["alice"], "/api/v1/tasks/"+taskID+change.action, change.body); status != http.StatusOK {
				t.Fatalf("%s by alice, who holds the task: got %d (%s), want 200", change.name, status, strings.TrimSpace(reply))
			}
			if assignee, owner, state := h.delegation(t, taskID); assignee != change.assignee || owner != change.owner || state != change.state {
				t.Fatalf("after %s the task is with %q, owned by %q, %q; want %q, %q, %q",
					change.name, assignee, owner, state, change.assignee, change.owner, change.state)
			}
			// Off the row, not only out of the reply: a client is not sent a
			// leftover mark whether or not it is still kept.
			if owner, state := h.storedDelegation(t, taskID); owner != change.owner || state != change.state {
				t.Fatalf("after %s the row keeps owner %q, %q; want %q, %q", change.name, owner, state, change.owner, change.state)
			}
		})
	}
}

// An edit of a task that is waiting to be handed back, or that has come back,
// changes what it carries and nothing about the delegation.
func TestAnEditLeavesALiveDelegationAlone(t *testing.T) {
	h := newTaskHarness(t)
	taskID := h.delegatedToMallory(t)
	path := "/api/v1/tasks/" + taskID

	if status, reply := h.do(t, http.MethodPut, h.tokens["mallory"], path, map[string]any{"priority": 70}); status != http.StatusOK {
		t.Fatalf("mallory, the delegate, editing the task: got %d (%s)", status, strings.TrimSpace(reply))
	}
	if assignee, owner, state := h.delegation(t, taskID); assignee != "mallory" || owner != "alice" || state != "pending" {
		t.Fatalf("after the delegate's edit the task is with %q, owned by %q, %q; want mallory, alice, pending", assignee, owner, state)
	}
	if status, reply := h.post(t, h.tokens["mallory"], path+"/resolve", map[string]any{}); status != http.StatusOK {
		t.Fatalf("mallory handing it back: %d (%s)", status, strings.TrimSpace(reply))
	}
	if status, reply := h.do(t, http.MethodPut, h.tokens["alice"], path, map[string]any{"priority": 80}); status != http.StatusOK {
		t.Fatalf("alice editing the task once it is back: got %d (%s)", status, strings.TrimSpace(reply))
	}
	if assignee, owner, state := h.delegation(t, taskID); assignee != "alice" || owner != "alice" || state != "resolved" {
		t.Fatalf("after the owner's edit the task is with %q, owned by %q, %q; want alice, alice, resolved", assignee, owner, state)
	}
}

// The denial for the listing across organizations: a task in another
// organization that names the caller as its owner is not the caller's to see.
// The same goes for a row that is delegated and owned but not pending, which
// is nothing its owner is waiting for.
func TestWhatSomebodyDelegatedIsListedOnlyInsideTheirOrganization(t *testing.T) {
	h := newTaskHarness(t)
	mine := h.delegatedToMallory(t)

	other, err := h.svc.CreateOrganization(context.Background(), "Another Org", "")
	if err != nil {
		t.Fatalf("create the other organization: %v", err)
	}
	otherCtx := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: other.ID.String()})
	project, err := h.svc.CreateProject(otherCtx, other.ID, "Another Project", "")
	if err != nil {
		t.Fatalf("create the other organization's project: %v", err)
	}
	repo := repositories.NewRepository(testutils.StormConn(h.db))
	pendingFor := func(ctx context.Context, projectID uuid.UUID, state entities.DelegationState) string {
		return seedTask(t, repo, ctx, projectID, entities.Task{
			Name: "Approve the refund", Type: entities.UserTask, Status: entities.TaskDelegated,
			Assignee:        &entities.User{Username: "mallory"},
			Owner:           &entities.User{Username: "alice"},
			DelegationState: state,
			Node:            &entities.Node{ID: "approve"},
		}).String()
	}
	theirs := pendingFor(otherCtx, project.ID, entities.DelegationPending)
	notPending := pendingFor(h.tenantContext(), h.projID, entities.DelegationResolved)

	got := h.delegatedBy(t, "alice")
	if slices.Contains(got, theirs) {
		t.Fatalf("alice is shown a task of another organization that names her as its owner: %v", got)
	}
	if slices.Contains(got, notPending) {
		t.Fatalf("alice is shown a delegated task that is not pending as one she is waiting for: %v", got)
	}
	if !slices.Equal(got, []string{mine}) {
		t.Fatalf("alice delegated %s in her organization and is shown %v", mine, got)
	}
	// Nor is it theirs to hand back: an administrator of the other organization
	// is told there is no such task, and it stays where it is.
	if err := h.svc.CreateUser(otherCtx, entities.User{
		Username:      "eve",
		Roles:         []string{entities.RoleAdmin},
		Organizations: []*entities.Organization{{ID: other.ID}},
	}, "actor-test-password"); err != nil {
		t.Fatalf("create the other organization's administrator: %v", err)
	}
	h.tokens["eve"] = h.login(t, "eve")
	if status, reply := h.post(t, h.tokens["eve"], "/api/v1/tasks/"+mine+"/resolve", map[string]any{"reason": "taking it back"}); status != http.StatusNotFound {
		t.Fatalf("another organization's administrator handing back this one's task: got %d (%s), want 404", status, strings.TrimSpace(reply))
	}
	if got := h.delegatedBy(t, "eve"); len(got) != 0 {
		t.Fatalf("eve delegated nothing and is shown %v", got)
	}
	if assignee, owner, state := h.delegation(t, mine); assignee != "mallory" || owner != "alice" || state != "pending" {
		t.Fatalf("after the refusal the task is with %q, owned by %q, %q; want mallory, alice, pending", assignee, owner, state)
	}

	// And the other organization's list has its own row, so the row was there
	// to be leaked.
	page, err := h.svc.ListTasksDelegatedByPaged(otherCtx, "alice", repocontracts.Pagination{Page: 1, PageSize: 50})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID.String() != theirs {
		t.Fatalf("the other organization lists %d tasks delegated by alice (%v), want its one", len(page.Items), err)
	}
}

// The row keeps a pending mark it is no longer waiting on until the next
// hand-over or edit takes it off. Until then a client is not sent it: whether
// a task is waiting to be handed back is decided here, once, and the inbox
// offers "Hand back" on what it is told is pending.
func TestAStalePendingMarkIsNotSentToAClient(t *testing.T) {
	h := newTaskHarness(t)

	claimed := h.staleMark(t)
	if owner, state := h.storedDelegation(t, claimed); owner != "mallory" || state != "pending" {
		t.Fatalf("the row carries owner %q, %q; the test needs it to carry mallory, pending", owner, state)
	}
	if assignee, owner, state := h.delegation(t, claimed); assignee != "alice" || owner != "" || state != "" {
		t.Fatalf("a task alice holds, with a leftover mark, is sent as with %q, owned by %q, %q; want alice and no delegation", assignee, owner, state)
	}

	// Withdrawn while it was with its delegate, as the engine withdraws: the
	// status alone.
	withdrawn := h.delegatedToMallory(t)
	if err := h.db.Exec(`UPDATE tasks SET status = 'canceled' WHERE id = ?`, withdrawn).Error; err != nil {
		t.Fatalf("withdraw the task as the engine does: %v", err)
	}
	if _, owner, state := h.delegation(t, withdrawn); owner != "" || state != "" {
		t.Fatalf("a withdrawn task is sent as owned by %q, %q; want no delegation", owner, state)
	}

	// The pin: a live delegation and one that came back are sent as they were.
	live := h.delegatedToMallory(t)
	if assignee, owner, state := h.delegation(t, live); assignee != "mallory" || owner != "alice" || state != "pending" {
		t.Fatalf("a task with its delegate is sent as with %q, owned by %q, %q; want mallory, alice, pending", assignee, owner, state)
	}
	if status, reply := h.post(t, h.tokens["mallory"], "/api/v1/tasks/"+live+"/resolve", map[string]any{}); status != http.StatusOK {
		t.Fatalf("mallory handing it back: %d (%s)", status, strings.TrimSpace(reply))
	}
	if assignee, owner, state := h.delegation(t, live); assignee != "alice" || owner != "alice" || state != "resolved" {
		t.Fatalf("a task that came back is sent as with %q, owned by %q, %q; want alice, alice, resolved", assignee, owner, state)
	}
}

// storedDelegation reads a task's owner and delegation state from its row —
// what is kept, which is not always what a client is sent.
func (h *taskHarness) storedDelegation(t *testing.T, id string) (string, string) {
	t.Helper()
	var row struct{ Owner, DelegationState string }
	if err := h.db.Raw(`SELECT COALESCE(owner, '') AS owner, COALESCE(delegation_state, '') AS delegation_state
		FROM tasks WHERE id = ?`, id).Scan(&row).Error; err != nil {
		t.Fatalf("read the task's row: %v", err)
	}
	return row.Owner, row.DelegationState
}
