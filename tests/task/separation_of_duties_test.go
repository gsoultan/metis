package task_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// Segregation of duties.
//
// "The person who requested it cannot be the person who approves it" is the
// oldest control there is, and a process graph cannot express it: the graph
// says a supervisor approves, not that the supervisor is somebody else. A node
// names the steps whose performer must not also perform it, and the engine
// refuses the second one.

// fourEyes is submit → approve, where approve may not be done by whoever
// submitted. Both steps are open to the same two people, which is exactly the
// shape the control exists for: nothing in the graph stops one of them doing
// both.
func fourEyes(projectID uuid.UUID, guarded bool) *entities.ProcessDefinition {
	approve := &entities.Node{
		ID: "approve", Name: "Approve", Type: entities.UserTask,
		CandidateUsers: []*entities.User{{Username: "ada"}, {Username: "bo"}},
		Incoming:       []string{"s1"}, Outgoing: []string{"s2"},
	}
	if guarded {
		approve.Properties = map[string]any{serviceimpl.SeparationOfDutiesKey: "submit"}
	}
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "four-eyes",
		Name:    "Four eyes",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"s0"}},
			{
				ID: "submit", Name: "Submit", Type: entities.UserTask,
				CandidateUsers: []*entities.User{{Username: "ada"}, {Username: "bo"}},
				Incoming:       []string{"s0"}, Outgoing: []string{"s1"},
			},
			approve,
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"s2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "s0", SourceRef: "start", TargetRef: "submit"},
			{ID: "s1", SourceRef: "submit", TargetRef: "approve"},
			{ID: "s2", SourceRef: "approve", TargetRef: "end"},
		},
	}
}

// TestTheSamePersonCannotDoBothHalvesOfAFourEyesCheck.
func TestTheSamePersonCannotDoBothHalvesOfAFourEyesCheck(t *testing.T) {
	h := newSoDHarness(t, true)

	h.complete(t, "submit", "ada")
	// Ada now tries to approve what Ada submitted.
	err := h.completeErr(t, "approve", "ada")
	if err == nil {
		t.Fatal("the same person completed both halves of a four-eyes check")
	}
	if !errors.Is(err, serviceimpl.ErrTaskForbidden) {
		t.Fatalf("expected a forbidden refusal, got %v", err)
	}

	// And somebody else can.
	if err := h.completeErr(t, "approve", "bo"); err != nil {
		t.Fatalf("a different person was refused the approval: %v", err)
	}
}

// TestTheConflictIsRefusedWhenTheWorkIsPickedUp, not only when it is finished:
// a task somebody can claim and never complete is a queue item that looks taken
// and is not.
func TestTheConflictIsRefusedWhenTheWorkIsPickedUp(t *testing.T) {
	h := newSoDHarness(t, true)
	h.complete(t, "submit", "ada")

	task := h.openTaskOn(t, "approve")
	if err := h.svc.ClaimTask(h.ctx, task.ID, "ada"); err == nil {
		t.Fatal("the submitter claimed the approval of their own submission")
	}
	if err := h.svc.ClaimTask(h.ctx, task.ID, "bo"); err != nil {
		t.Fatalf("a different person was refused the claim: %v", err)
	}
}

// TestWithoutTheConstraintTheSamePersonMayDoBoth keeps it opt-in: a node that
// declares nothing behaves exactly as it always did.
func TestWithoutTheConstraintTheSamePersonMayDoBoth(t *testing.T) {
	h := newSoDHarness(t, false)
	h.complete(t, "submit", "ada")
	if err := h.completeErr(t, "approve", "ada"); err != nil {
		t.Fatalf("an unguarded node refused the same person: %v", err)
	}
}

// TestCompletingRecordsWhoDidIt is what the control is built on: a task routed
// by candidate group used to complete with no assignee, so the row said a step
// had happened and not by whom.
func TestCompletingRecordsWhoDidIt(t *testing.T) {
	h := newSoDHarness(t, true)
	h.complete(t, "submit", "ada")

	tasks, err := h.svc.ListTasks(h.ctx, h.project)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.NodeID() != "submit" {
			continue
		}
		if task.Assignee == nil || task.Assignee.Username != "ada" {
			got := "<nobody>"
			if task.Assignee != nil {
				got = task.Assignee.Username
			}
			t.Fatalf("the completed task records %q as having done it; ada did", got)
		}
		return
	}
	t.Fatal("no submit task found")
}

// soDHarness is a full facade: these tests start a process and complete real
// tasks, which the task-service-only harness in this package cannot do.
type soDHarness struct {
	svc     services.ServiceFacade
	ctx     context.Context
	project uuid.UUID
}

func newSoDHarness(t *testing.T, guarded bool) *soDHarness {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), observersimpl.NewSSEObserver(),
		"sod-test", nil, nil, nil)

	ctx := context.Background()
	org, err := svc.CreateOrganization(ctx, "Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	tenantCtx := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})
	project, err := svc.CreateProject(tenantCtx, org.ID, "P", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	h := &soDHarness{svc: svc, ctx: tenantCtx, project: project.ID}

	def := fourEyes(project.ID, guarded)
	if _, err := svc.CreateDefinition(tenantCtx, def); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if _, err := svc.StartProcess(tenantCtx, project.ID, "four-eyes", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	return h
}

func (h *soDHarness) openTaskOn(t *testing.T, nodeID string) entities.Task {
	t.Helper()
	tasks, err := h.svc.ListTasks(h.ctx, h.project)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.NodeID() != nodeID {
			continue
		}
		switch task.Status {
		case entities.TaskUnclaimed, entities.TaskClaimed, entities.TaskDelegated:
			return task
		}
	}
	t.Fatalf("no open task on %q", nodeID)
	return entities.Task{}
}

func (h *soDHarness) completeErr(t *testing.T, nodeID, actor string) error {
	t.Helper()
	return h.svc.CompleteTask(h.ctx, h.openTaskOn(t, nodeID).ID, actor, nil)
}

func (h *soDHarness) complete(t *testing.T, nodeID, actor string) {
	t.Helper()
	if err := h.completeErr(t, nodeID, actor); err != nil {
		t.Fatalf("complete %q as %s: %v", nodeID, actor, err)
	}
}
