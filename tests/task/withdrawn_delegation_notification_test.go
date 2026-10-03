package task_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/tests/testutils"
)

// twoOfFourApprovals starts an approval four people are asked for and two are
// enough to settle, and returns its four open tasks in the order they were
// asked. Nobody is named for them, so they are the operators' to take.
func (h *taskHarness) twoOfFourApprovals(t *testing.T) []string {
	t.Helper()
	ctx := h.tenantContext()
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "two-of-four",
		Name:    "Purchase approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{
				ID: "approve", Type: entities.UserTask, Name: "Approve the purchase",
				MultiInstanceType: "parallel", Collection: "approvers", ElementVariable: "approver",
				CompletionCondition: "nrOfCompletedInstances >= 2",
				Properties:          testutils.FormDeclaring("decision"),
			},
			{ID: "record", Type: entities.UserTask, Name: "Record the outcome"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "record"},
			{ID: "f3", SourceRef: "record", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, def); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	instanceID, err := h.svc.StartProcess(ctx, h.projID, def.Key, map[string]any{"approvers": []any{"first", "second", "third", "fourth"}})
	if err != nil {
		t.Fatalf("start the approval: %v", err)
	}
	page, err := h.svc.ListTasksByInstancePaged(ctx, instanceID, repocontracts.Pagination{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatalf("list the approval's tasks: %v", err)
	}
	if len(page.Items) != 4 {
		t.Fatalf("four approvers were asked and %d tasks are open", len(page.Items))
	}
	slices.SortFunc(page.Items, func(a, b entities.Task) int { return strings.Compare(a.IterationID, b.IterationID) })
	ids := make([]string, len(page.Items))
	for i, task := range page.Items {
		ids[i] = task.ID.String()
	}
	return ids
}

// A task with a delegate has two people waiting on it: the delegate, who is
// doing the work, and the owner, who is waiting to have it back. The engine
// withdrawing it — the approval's rule was met without it — told the delegate
// and left the owner watching for a hand-back that was never going to come.
func TestTheOwnerOfAWithdrawnDelegationIsToldAsTheDelegateIs(t *testing.T) {
	h := newTaskHarness(t)
	for _, operator := range []string{"ana", "olga", "carol"} {
		h.tokens[operator] = h.signInWithRoles(t, operator, entities.RoleOperator)
	}
	approvals := h.twoOfFourApprovals(t)
	path := func(i int, action string) string { return "/api/v1/tasks/" + approvals[i] + "/" + action }
	mustPost := func(who, path string, body map[string]any) {
		t.Helper()
		if status, reply := h.post(t, h.tokens[who], path, body); status != http.StatusOK {
			t.Fatalf("%s posting %s: %d (%s)", who, path, status, strings.TrimSpace(reply))
		}
	}

	// The third approval is ana's, and she has delegated it to mallory. The
	// fourth is olga's own.
	mustPost("ana", path(2, "claim"), map[string]any{})
	mustPost("ana", path(2, "delegate"), map[string]any{"user_id": "mallory"})
	mustPost("olga", path(3, "claim"), map[string]any{})

	// Two approvals are enough: the engine withdraws the other two.
	mustPost("carol", path(0, "complete"), map[string]any{})
	mustPost("carol", path(1, "complete"), map[string]any{})
	for _, i := range []int{2, 3} {
		if got := h.taskStatus(t, approvals[i]); got != string(entities.TaskCanceled) {
			t.Fatalf("approval %d is %q once two were given, want it withdrawn", i+1, got)
		}
	}

	// The pin: a withdrawn task nobody delegated tells its holder, once, as it
	// did, and what the delegate of one is told has not changed either.
	const claimed, withdrawn = "Task Update", "A task was withdrawn"
	if got := h.notificationTitles(t, "olga"); !slices.Equal(got, []string{claimed, withdrawn}) {
		t.Fatalf("olga's own approval was withdrawn and she has been sent %v; want %q", got, []string{claimed, withdrawn})
	}
	if got := h.notificationTitles(t, "mallory"); !slices.Equal(got, []string{"A task was delegated to you", withdrawn}) {
		t.Fatalf("the approval mallory was working on was withdrawn and she has been sent %v", got)
	}
	// And the owner is told what the delegate is.
	if got := h.notificationTitles(t, "ana"); !slices.Equal(got, []string{claimed, withdrawn}) {
		t.Fatalf("the approval ana delegated was withdrawn and she has been sent %v; want %q", got, []string{claimed, withdrawn})
	}
	// Nobody who was not waiting on either is told anything about them.
	if got := h.notificationTitles(t, "carol"); slices.Contains(got, withdrawn) {
		t.Fatalf("carol held neither withdrawn approval and was sent %v", got)
	}
}
