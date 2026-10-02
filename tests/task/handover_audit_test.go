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
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// trail returns an instance's audit entries of one type, oldest first.
func (h *taskHarness) trail(t *testing.T, instanceID uuid.UUID, auditType string) []models.AuditModel {
	t.Helper()
	repo := repositories.NewRepository(testutils.StormConn(h.db))
	entries, err := repo.Audit().ListByInstance(h.tenantContext(), instanceID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	var of []models.AuditModel
	for _, entry := range entries {
		if entry.Type == auditType {
			of = append(of, entry)
		}
	}
	return of
}

// lastOf is the newest entry of a type, which the test expects to be there.
func (h *taskHarness) lastOf(t *testing.T, instanceID uuid.UUID, auditType string) models.AuditModel {
	t.Helper()
	entries := h.trail(t, instanceID, auditType)
	if len(entries) == 0 {
		t.Fatalf("the trail has no %s entry", auditType)
	}
	return entries[len(entries)-1]
}

// An assignment's entry named the person the task went to as its actor, a
// delegation's did the same, and a release's named nobody. An administrator
// taking an approval off one person and giving it to another left a trail in
// which the administrator does not appear.
func TestTheTrailNamesWhoMadeAHandOverFromWhomToWhomAndWhy(t *testing.T) {
	h := newTaskHarness(t)
	boss := h.signInAdministrator(t, "boss")
	h.tokens["bob"] = h.signInWithRoles(t, "bob", entities.RoleUser)
	taskID, instanceID := h.openTaskWith(t, heldByAlice(), nil)
	path := "/api/v1/tasks/" + taskID

	if status, reply := h.post(t, boss, path+"/assign", map[string]any{"user_id": "bob", "reason": "alice is on leave until Monday"}); status != http.StatusOK {
		t.Fatalf("assign: %d (%s)", status, reply)
	}
	assigned := h.lastOf(t, instanceID, "task_assigned")
	for key, want := range map[string]any{"actor": "boss", "target": "bob", "previous_holder": "alice", "reason": "alice is on leave until Monday"} {
		if assigned.Data[key] != want {
			t.Errorf("the assignment's entry records %s = %v, want %v (%v)", key, assigned.Data[key], want, assigned.Data)
		}
	}
	if want := `boss reassigned task "Approve the refund" from alice to bob: alice is on leave until Monday`; assigned.Narrative != want {
		t.Errorf("the assignment reads %q, want %q", assigned.Narrative, want)
	}

	if status, reply := h.post(t, h.tokens["bob"], path+"/delegate", map[string]any{"user_id": "mallory"}); status != http.StatusOK {
		t.Fatalf("delegate: %d (%s)", status, reply)
	}
	delegated := h.lastOf(t, instanceID, "task_delegated")
	if delegated.Data["actor"] != "bob" || delegated.Data["target"] != "mallory" || delegated.Data["previous_holder"] != "bob" {
		t.Errorf("the delegation's entry records %v, want actor bob, target mallory, previous_holder bob", delegated.Data)
	}
	if _, has := delegated.Data["reason"]; has {
		t.Errorf("the holder gave no reason and the entry records one: %v", delegated.Data)
	}
	if want := `bob delegated task "Approve the refund" to mallory`; delegated.Narrative != want {
		t.Errorf("the delegation reads %q, want %q", delegated.Narrative, want)
	}
}

func TestTheTrailNamesWhoReleasedATask(t *testing.T) {
	h := newTaskHarness(t)
	boss := h.signInAdministrator(t, "boss")
	taskID, instanceID := h.openTaskWith(t, heldByAlice(), nil)

	if status, reply := h.post(t, boss, "/api/v1/tasks/"+taskID+"/unclaim", map[string]any{"reason": "alice left the company"}); status != http.StatusOK {
		t.Fatalf("release: %d (%s)", status, reply)
	}
	released := h.lastOf(t, instanceID, "task_unclaimed")
	if released.Data["actor"] != "boss" || released.Data["previous_holder"] != "alice" || released.Data["reason"] != "alice left the company" {
		t.Errorf("the release's entry records %v, want actor boss, previous_holder alice and the reason", released.Data)
	}
	if want := `boss released task "Approve the refund" from alice back to the queue: alice left the company`; released.Narrative != want {
		t.Errorf("the release reads %q, want %q", released.Narrative, want)
	}
}

// refusingAuditWriter cannot write.
type refusingAuditWriter struct{}

func (refusingAuditWriter) RecordEvent(context.Context, entities.AuditEntry) error {
	return errors.New("the audit table is not there")
}

// A hand-over's entry was written after the fact and its failure was logged
// and forgotten, so a task could change hands with nothing in the trail to say
// who moved it. The entry is part of the hand-over now: if it cannot be
// written, the task does not move.
func TestAHandOverThatCannotBeRecordedIsNotMade(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	engine := serviceimpl.NewExecutionEngine(repo, observerimpl.NewEventDispatcher())
	svc := serviceimpl.NewTaskService(repo, engine, refusingAuditWriter{})
	ctx, _, projectID := testutils.ScopedProject(t, repo)
	seedMember(t, repo, ctx, "alice")
	seedMember(t, repo, ctx, "bob")
	taskID := seedTask(t, repo, ctx, projectID, entities.Task{
		Name: "Approve the refund", Type: entities.UserTask, Status: entities.TaskClaimed,
		Assignee: &entities.User{Username: "alice"}, Node: &entities.Node{ID: "approve"}, Instance: seedCase(t, repo, ctx, projectID),
	})

	err := svc.AssignTask(ctx, taskID, servicecontracts.HandOver{Actor: "alice", Target: "bob"})
	if err == nil || !strings.Contains(err.Error(), "not recorded") {
		t.Fatalf("an assignment whose entry could not be written: got %v, want it refused as not recorded", err)
	}
	if err := svc.UnclaimTask(ctx, taskID, servicecontracts.HandOver{Actor: "alice"}); err == nil {
		t.Fatal("a release whose entry could not be written was made")
	}
	task, err := svc.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("read the task: %v", err)
	}
	if task.AssigneeUsername() != "alice" || task.Status != entities.TaskClaimed {
		t.Fatalf("the task is %s and held by %q; it should not have moved", task.Status, task.AssigneeUsername())
	}
}
