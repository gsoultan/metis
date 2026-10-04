package task_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	observerimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/tests/testutils"
)

// ledgerOf is the instance's deviation ledger, oldest first.
func (h *taskHarness) ledgerOf(t *testing.T, instanceID uuid.UUID) []entities.Deviation {
	t.Helper()
	rows, err := repositories.NewRepository(testutils.StormConn(h.db)).Deviation().ListByInstance(h.tenantContext(), instanceID)
	if err != nil {
		t.Fatalf("read the ledger: %v", err)
	}
	return rows
}

func lastRow(t *testing.T, rows []entities.Deviation, kind entities.DeviationKind) entities.Deviation {
	t.Helper()
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Kind == kind {
			return rows[i]
		}
	}
	t.Fatalf("the ledger has no %s row: %+v", kind, rows)
	return entities.Deviation{}
}

func taskFields(d entities.Deviation, section string) map[string]any {
	side := d.Before
	if section == "after" {
		side = d.After
	}
	tasks, _ := side["tasks"].(map[string]any)
	for _, fields := range tasks {
		m, _ := fields.(map[string]any)
		return m
	}
	return nil
}

// An administrator taking work from one person and giving it to another, an
// administrator releasing it, handing it back for a delegate or editing it is
// a deviation from what the process decided. Each is now a ledger row beside
// slice 2's entry, in the same transaction, the two pointing at each other.
func TestEveryHandOverByANonHolderIsLedgered(t *testing.T) {
	h := newTaskHarness(t)
	boss := h.signInAdministrator(t, "boss")
	h.tokens["bob"] = h.signInWithRoles(t, "bob", entities.RoleUser)
	taskID, instanceID := h.openTaskWith(t, heldByAlice(), nil)
	path := "/api/v1/tasks/" + taskID

	steps := []struct {
		method, suffix string
		body           map[string]any
		kind           entities.DeviationKind
		audit          string
	}{
		{http.MethodPost, "/assign", map[string]any{"user_id": "bob", "reason": "alice is on leave"}, entities.DeviationReassign, "task_assigned"},
		{http.MethodPut, "", map[string]any{"name": "Approve the refund today", "reason": "the customer escalated"}, entities.DeviationTaskEdit, "task_edited"},
		{http.MethodPost, "/unclaim", map[string]any{"reason": "bob is off sick"}, entities.DeviationRelease, "task_unclaimed"},
	}
	for _, step := range steps {
		if status, reply := h.do(t, step.method, boss, path+step.suffix, step.body); status != http.StatusOK {
			t.Fatalf("%s%s: %d (%s)", step.method, step.suffix, status, reply)
		}
		row := lastRow(t, h.ledgerOf(t, instanceID), step.kind)
		entry := h.lastOf(t, instanceID, step.audit)
		if row.Actor != "boss" || row.Reason != step.body["reason"] || row.Origin != entities.DeviationOriginTask ||
			row.Task == nil || row.Task.ID.String() != taskID || row.Node == nil || row.Node.ID != "step" {
			t.Errorf("%s row: %+v", step.kind, row)
		}
		if row.AuditEntryID != uuid.UUID(entry.ID) || entry.Data["deviation_id"] != row.ID.String() {
			t.Errorf("%s: the row names entry %s and the entry names row %v; they should name each other (entry %s, row %s)",
				step.kind, row.AuditEntryID, entry.Data["deviation_id"], uuid.UUID(entry.ID), row.ID)
		}
	}
	reassigned := lastRow(t, h.ledgerOf(t, instanceID), entities.DeviationReassign)
	if taskFields(reassigned, "before")["assignee"] != "alice" || taskFields(reassigned, "after")["assignee"] != "bob" {
		t.Errorf("the reassignment's before/after: %v → %v", reassigned.Before, reassigned.After)
	}
	edited := lastRow(t, h.ledgerOf(t, instanceID), entities.DeviationTaskEdit)
	if taskFields(edited, "after")["name"] != "Approve the refund today" {
		t.Errorf("the edit's after: %v", edited.After)
	}

	// Resolve: alice claims and delegates (her own task, no row), boss hands it back.
	second, secondInstance := h.openTaskWith(t, heldByAlice(), nil)
	if status, reply := h.post(t, h.tokens["alice"], "/api/v1/tasks/"+second+"/delegate", map[string]any{"user_id": "mallory"}); status != http.StatusOK {
		t.Fatalf("delegate: %d (%s)", status, reply)
	}
	if status, reply := h.post(t, boss, "/api/v1/tasks/"+second+"/resolve", map[string]any{"reason": "mallory is away"}); status != http.StatusOK {
		t.Fatalf("resolve: %d (%s)", status, reply)
	}
	rows := h.ledgerOf(t, secondInstance)
	if len(rows) != 1 || rows[0].Kind != entities.DeviationResolve || rows[0].Actor != "boss" {
		t.Fatalf("the hand-back's ledger: %+v, want one resolve by boss and nothing for alice's own delegation", rows)
	}
}

// Somebody handing on, releasing or editing their own task explains nothing to
// anybody and deviates from nothing: no row.
func TestAHolderHandingOnTheirOwnTaskWritesNoLedgerRow(t *testing.T) {
	h := newTaskHarness(t)
	h.tokens["bob"] = h.signInWithRoles(t, "bob", entities.RoleUser)
	taskID, instanceID := h.openTaskWith(t, heldByAlice(), nil)
	path := "/api/v1/tasks/" + taskID
	if status, reply := h.do(t, http.MethodPut, h.tokens["alice"], path, map[string]any{"priority": 40}); status != http.StatusOK {
		t.Fatalf("edit: %d (%s)", status, reply)
	}
	if status, reply := h.post(t, h.tokens["alice"], path+"/assign", map[string]any{"user_id": "bob"}); status != http.StatusOK {
		t.Fatalf("assign: %d (%s)", status, reply)
	}
	if status, reply := h.post(t, h.tokens["bob"], path+"/unclaim", map[string]any{}); status != http.StatusOK {
		t.Fatalf("release: %d (%s)", status, reply)
	}
	if rows := h.ledgerOf(t, instanceID); len(rows) != 0 {
		t.Fatalf("holders acting on their own task wrote %d ledger row(s): %+v", len(rows), rows)
	}
}

// The ledger asks a reason of every hand-over kind; slice 2 asks one only of
// somebody who does not hold the task. So a row is written exactly when slice
// 2 asked: a holder's reasonless hand-over is made and writes none, where a
// row would have been refused for its missing reason and taken the hand-over
// with it; the same by an administrator, with a reason, writes exactly one.
func TestOnlyAHandOverThatNeededAReasonIsLedgered(t *testing.T) {
	h := newTaskHarness(t)
	boss := h.signInAdministrator(t, "boss")
	h.tokens["bob"] = h.signInWithRoles(t, "bob", entities.RoleUser)
	cases := []struct {
		name, method, suffix string
		body                 map[string]any
		kind                 entities.DeviationKind
	}{
		{"reassign", http.MethodPost, "/assign", map[string]any{"user_id": "bob"}, entities.DeviationReassign},
		{"delegate", http.MethodPost, "/delegate", map[string]any{"user_id": "bob"}, entities.DeviationDelegate},
		{"release", http.MethodPost, "/unclaim", map[string]any{}, entities.DeviationRelease},
		{"edit", http.MethodPut, "", map[string]any{"priority": 40}, entities.DeviationTaskEdit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			own, ownInstance := h.openTaskWith(t, heldByAlice(), nil)
			if status, reply := h.do(t, c.method, h.tokens["alice"], "/api/v1/tasks/"+own+c.suffix, c.body); status != http.StatusOK {
				t.Fatalf("alice's own %s with no reason: %d (%s)", c.name, status, reply)
			}
			if rows := h.ledgerOf(t, ownInstance); len(rows) != 0 {
				t.Fatalf("alice's own %s wrote %d ledger row(s): %+v", c.name, len(rows), rows)
			}

			theirs, theirInstance := h.openTaskWith(t, heldByAlice(), nil)
			withReason := map[string]any{"reason": "alice is on leave"}
			for key, value := range c.body {
				withReason[key] = value
			}
			if status, reply := h.do(t, c.method, boss, "/api/v1/tasks/"+theirs+c.suffix, withReason); status != http.StatusOK {
				t.Fatalf("boss's %s of alice's task: %d (%s)", c.name, status, reply)
			}
			rows := h.ledgerOf(t, theirInstance)
			if len(rows) != 1 || rows[0].Kind != c.kind || rows[0].Actor != "boss" || rows[0].Reason != "alice is on leave" {
				t.Fatalf("boss's %s wrote %+v, want exactly one %s row by boss with his reason", c.name, rows, c.kind)
			}
		})
	}
}

// An administrator who holds the task needs no reason to hand it on — unless
// they send it to somebody it was not offered to. That override is a row.
func TestAnAdministratorHoldersOverrideIsLedgered(t *testing.T) {
	h := newTaskHarness(t)
	boss := h.signInAdministrator(t, "boss")
	h.tokens["bob"] = h.signInWithRoles(t, "bob", entities.RoleUser)
	taskID, instanceID := h.openTaskWith(t, entities.Node{
		Name: "Approve the refund", Type: entities.UserTask, Assignee: "boss",
		CandidateUsers: []*entities.User{{Username: "boss"}, {Username: "alice"}},
		Properties:     testutils.FormDeclaring("approved"),
	}, nil)
	if status, reply := h.post(t, boss, "/api/v1/tasks/"+taskID+"/assign", map[string]any{"user_id": "bob", "reason": "only bob knows this customer"}); status != http.StatusOK {
		t.Fatalf("assign: %d (%s)", status, reply)
	}
	row := lastRow(t, h.ledgerOf(t, instanceID), entities.DeviationReassign)
	if row.Details["override"] != "not_a_candidate" || row.Reason != "only bob knows this customer" {
		t.Fatalf("the override's row: %+v", row)
	}
}

// refusingDeviations stands for a ledger that cannot be written.
type refusingDeviations struct{}

func (refusingDeviations) Create(context.Context, entities.Deviation) (entities.Deviation, error) {
	return entities.Deviation{}, errors.New("the ledger table is not there")
}

func (refusingDeviations) ListByInstance(context.Context, uuid.UUID) ([]entities.Deviation, error) {
	return nil, nil
}

func (refusingDeviations) FindLiveByVisit(context.Context, uuid.UUID, string) (entities.Deviation, bool, error) {
	return entities.Deviation{}, false, nil
}

type unledgeredRepository struct{ repositories.Repository }

func (unledgeredRepository) Deviation() repocontracts.DeviationRepository {
	return refusingDeviations{}
}

// A hand-over that needs a ledger row and cannot have one is not made: an
// override nobody can find later is the thing the ledger exists to prevent.
func TestAHandOverThatCannotBeLedgeredIsNotMade(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	engine := serviceimpl.NewExecutionEngine(repo, observerimpl.NewEventDispatcher())
	svc := serviceimpl.NewTaskService(unledgeredRepository{repo}, engine, serviceimpl.NewAuditWriter(repo.Audit()))
	ctx, _, projectID := testutils.ScopedProject(t, repo)
	seedMember(t, repo, ctx, "alice")
	instance := seedCase(t, repo, ctx, projectID)
	taskID := seedTask(t, repo, ctx, projectID, entities.Task{
		Name: "Approve the refund", Type: entities.UserTask, Status: entities.TaskClaimed,
		Assignee: &entities.User{Username: "alice"}, Node: &entities.Node{ID: "approve"}, Instance: instance,
	})

	err := svc.UnclaimTask(asAdministrator(ctx, "boss"), taskID, servicecontracts.HandOver{Actor: "boss", Reason: "alice left"})
	if err == nil || !strings.Contains(err.Error(), "not made") {
		t.Fatalf("a release whose ledger row could not be written: got %v, want it refused as not made", err)
	}
	task, err := svc.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("read the task: %v", err)
	}
	if task.AssigneeUsername() != "alice" || task.Status != entities.TaskClaimed {
		t.Fatalf("the task is %s held by %q; it should not have moved", task.Status, task.AssigneeUsername())
	}
	entries, err := repo.Audit().ListByInstance(ctx, instance.ID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	for _, entry := range entries {
		if entry.Type == "task_unclaimed" {
			t.Fatalf("the trail records a release that was not made: %+v", entry)
		}
	}
	// The holder's own release needs no row, so a missing ledger does not stop it.
	if err := svc.UnclaimTask(ctx, taskID, servicecontracts.HandOver{Actor: "alice"}); err != nil {
		t.Fatalf("alice releasing her own task with no ledger: %v", err)
	}
}

// Review Focus 1. The hand-over holds the task's row and is held at the ledger
// insert; the holder's completion takes the instance and waits for the row.
// With a foreign key from the ledger to the instance the insert would then
// wait for the completion and the completion for the insert: a deadlock,
// answered 500. Without one the hand-over finishes and the completion is
// refused — the task is no longer the completer's.
func TestANonHolderHandOverInFlightIsNotDeadlockedByACompletion(t *testing.T) {
	h := newTaskHarness(t)
	boss := h.signInAdministrator(t, "boss")
	taskID, instanceID := h.openTaskWith(t, heldByAlice(), nil)
	path := "/api/v1/tasks/" + taskID

	release := h.hold(t, "instance_deviations")
	handOver := make(chan answer, 1)
	go func() {
		handOver <- h.send(boss, path+"/assign", map[string]any{"user_id": "mallory", "reason": "alice must not approve this one"})
	}()
	handingOver := h.heldAt(t, "instance_deviations")

	completion := make(chan answer, 1)
	go func() {
		completion <- h.send(h.tokens["alice"], path+"/complete", map[string]any{"variables": map[string]any{"approved": true}})
	}()
	completed, answered := h.doneOrWaitingBehind(t, handingOver, completion)
	release()
	if !answered {
		completed = <-completion
	}
	handedOver := <-handOver
	if completed.err != nil || handedOver.err != nil {
		t.Fatalf("the completion: %v; the hand-over: %v", completed, handedOver)
	}
	for name, a := range map[string]answer{"completion": completed, "hand-over": handedOver} {
		if a.status >= 500 || strings.Contains(a.body, "deadlock") {
			t.Fatalf("the %s was answered %v; a race between the two is decided, not crashed", name, a)
		}
	}
	if handedOver.status != http.StatusOK || completed.status != http.StatusForbidden {
		t.Fatalf("hand-over %v, completion %v; want the hand-over made and alice told the task is no longer hers", handedOver, completed)
	}
	if status := h.taskStatus(t, taskID); status == string(entities.TaskCompleted) {
		t.Fatalf("the task was completed by alice after it was taken from her")
	}
	if rows := h.ledgerOf(t, instanceID); len(rows) != 1 || rows[0].Kind != entities.DeviationReassign {
		t.Fatalf("the ledger after the race: %+v, want one reassignment", rows)
	}
}

// A step may be named at greater length than the ledger keeps. A hand-over of
// its task is still ledgered: the row carries as much of the name as fits —
// the step's id is what identifies it — rather than the hand-over failing on a
// name nobody at the task could change.
func TestAHandOverOfAStepWithAVeryLongNameIsStillLedgered(t *testing.T) {
	h := newTaskHarness(t)
	boss := h.signInAdministrator(t, "boss")
	h.tokens["bob"] = h.signInWithRoles(t, "bob", entities.RoleUser)
	step := heldByAlice()
	step.Name = string([]rune(strings.Repeat("persetujuan — ", 22))[:300])
	taskID, instanceID := h.openTaskWith(t, step, nil)

	status, reply := h.post(t, boss, "/api/v1/tasks/"+taskID+"/assign", map[string]any{"user_id": "bob", "reason": "alice is on leave"})
	if status != http.StatusOK {
		t.Fatalf("assign: %d (%s)", status, reply)
	}
	row := lastRow(t, h.ledgerOf(t, instanceID), entities.DeviationReassign)
	if row.Node == nil || row.Node.ID != "step" {
		t.Fatalf("the reassignment's row: %+v", row)
	}
	if kept := []rune(row.Node.Name); len(kept) != 255 || !strings.HasPrefix(step.Name, row.Node.Name) {
		t.Errorf("the row keeps %d characters of the step's name, want the first 255", len(kept))
	}
}
