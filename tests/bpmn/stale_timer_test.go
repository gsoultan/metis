package bpmn_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// Timers that should no longer fire.
//
// A boundary timer is a deadline: "escalate if approval takes more than two
// hours". If the approver answers in ten minutes the deadline is moot. An
// event-based gateway is a race: whichever event arrives first wins and the
// losing branches are off.
//
// Both cancel by removing the token and deleting the subscription — but a timer
// branch is a job, not a subscription, and JobRepository has no way to cancel
// one. So the pending job survives, and when it comes due it executes its node
// on an instance that moved on without it.

// dueNow drags every pending job for the instance into the past, standing in for
// the wall-clock time passing between the activity finishing and the deadline
// arriving.
func (h engineHarness) dueNow(ctx context.Context, t *testing.T, instanceID uuid.UUID) int {
	t.Helper()

	jobs, err := h.repo.Job().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	moved := 0
	for _, job := range jobs {
		if job.Status != "pending" {
			continue
		}
		job.NextRunAt = time.Now().Add(-time.Minute)
		if err := h.repo.Job().Update(ctx, job); err != nil {
			t.Fatalf("bring job forward: %v", err)
		}
		moved++
	}
	return moved
}

// A deadline that has already been met must not fire.
func TestBoundaryTimerDoesNotFireAfterItsActivityCompleted(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Stale Boundary Timer Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "approval-with-deadline",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "approve", Type: entities.UserTask, Name: "Approve the request"},
			{ID: "deadline", Type: entities.BoundaryEvent, AttachedToRef: "approve", Properties: map[string]any{
				"timer_duration": "PT2H",
			}},
			{ID: "escalate", Type: entities.UserTask, Name: "Escalate to the manager"},
			{ID: "done", Type: entities.UserTask, Name: "Record the decision"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "done"},
			{ID: "f3", SourceRef: "done", TargetRef: "end"},
			{ID: "f4", SourceRef: "deadline", TargetRef: "escalate"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "approval-with-deadline", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	// The approver answers well inside the deadline.
	tasks, err := h.svc.ListTasks(ctx, h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	approved := false
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == "approve" {
			if err := h.svc.CompleteTask(ctx, task.ID, "carol", nil); err != nil {
				t.Fatalf("approve: %v", err)
			}
			approved = true
		}
	}
	if !approved {
		t.Fatal("the process never offered the approval task")
	}

	// Two hours pass.
	if moved := h.dueNow(ctx, t, instanceID); moved == 0 {
		t.Skip("no pending timer job was left to bring forward")
	}
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if h.waitingAt(ctx, t, instanceID, "escalate") {
		t.Error("the deadline fired even though the approval was completed in time")
	}
}

// The losing branch of an event-based gateway must stay off.
func TestEventGatewayTimerBranchDoesNotFireAfterAnotherBranchWon(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Stale Gateway Timer Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "payment-race",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "race", Type: entities.EventBasedGateway},
			{ID: "await-payment", Type: entities.IntermediateCatchEvent, Properties: map[string]any{
				"message_name": "PaymentReceived",
			}},
			{ID: "await-timeout", Type: entities.IntermediateCatchEvent, Properties: map[string]any{
				"timer_duration": "PT1H",
			}},
			{ID: "ship", Type: entities.UserTask, Name: "Ship the order"},
			{ID: "cancel", Type: entities.UserTask, Name: "Cancel the order"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "race"},
			{ID: "f2", SourceRef: "race", TargetRef: "await-payment"},
			{ID: "f3", SourceRef: "race", TargetRef: "await-timeout"},
			{ID: "f4", SourceRef: "await-payment", TargetRef: "ship"},
			{ID: "f5", SourceRef: "await-timeout", TargetRef: "cancel"},
			{ID: "f6", SourceRef: "ship", TargetRef: "end"},
			{ID: "f7", SourceRef: "cancel", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "payment-race", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	// Payment wins the race.
	if err := h.engine.SendMessage(ctx, h.projID, "PaymentReceived", "", nil); err != nil {
		t.Fatalf("send the payment message: %v", err)
	}
	if !h.waitingAt(ctx, t, instanceID, "ship") {
		t.Fatal("the payment branch did not win the race")
	}

	// The hour would have passed.
	if moved := h.dueNow(ctx, t, instanceID); moved == 0 {
		t.Skip("no pending timer job was left to bring forward")
	}
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if h.waitingAt(ctx, t, instanceID, "cancel") {
		t.Error("the timeout branch fired even though the payment branch had already won")
	}
}

// The guard must not block a deadline that is genuinely missed: the activity is
// still open when the timer comes due, so the escalation is exactly right.
func TestBoundaryTimerStillFiresWhileItsActivityIsOpen(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Live Boundary Timer Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "approval-missed-deadline",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "approve", Type: entities.UserTask, Name: "Approve the request"},
			{ID: "deadline", Type: entities.BoundaryEvent, AttachedToRef: "approve", Properties: map[string]any{
				"timer_duration": "PT2H",
			}},
			{ID: "escalate", Type: entities.UserTask, Name: "Escalate to the manager"},
			{ID: "done", Type: entities.UserTask, Name: "Record the decision"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "done"},
			{ID: "f3", SourceRef: "done", TargetRef: "end"},
			{ID: "f4", SourceRef: "deadline", TargetRef: "escalate"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "approval-missed-deadline", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	// Nobody approves. The two hours pass with the task still open.
	if moved := h.dueNow(ctx, t, instanceID); moved == 0 {
		t.Fatal("no pending timer job was scheduled for the deadline")
	}
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if !h.waitingAt(ctx, t, instanceID, "escalate") {
		t.Error("the deadline was missed but the escalation never happened")
	}
}

// A plain intermediate timer with nothing racing it must still advance.
func TestIntermediateTimerStillAdvancesTheProcess(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Live Intermediate Timer Project")

	def := timerDefinition("cooling-off-fires", "PT10M")
	def.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "cooling-off-fires", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	if moved := h.dueNow(ctx, t, instanceID); moved == 0 {
		t.Fatal("no pending timer job was scheduled")
	}
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if !h.waitingAt(ctx, t, instanceID, "ship") {
		t.Error("the cooling-off period elapsed but the process did not carry on")
	}
}

// A non-interrupting boundary event notifies without stopping the work.
//
// The engine used to remove the attached activity's token whenever any boundary
// event fired, so every boundary event was interrupting whatever the model said.
// It cannot be driven from the CancelActivity field: that is a plain bool the
// designer writes as false unconditionally, and BPMN's default is interrupting,
// so honouring it would stop an error boundary cancelling the activity that
// failed. It is an explicit opt-in property instead.
func TestNonInterruptingBoundaryEventLeavesTheActivityRunning(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Non Interrupting Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "approval-non-interrupting",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "approve", Type: entities.UserTask, Name: "Approve the request"},
			{ID: "nudge", Type: entities.BoundaryEvent, AttachedToRef: "approve", Properties: map[string]any{
				"timer_duration":   "PT2H",
				"non_interrupting": true,
			}},
			{ID: "send-nudge", Type: entities.UserTask, Name: "Send a nudge"},
			{ID: "done", Type: entities.UserTask, Name: "Record the decision"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "done"},
			{ID: "f3", SourceRef: "done", TargetRef: "end"},
			{ID: "f4", SourceRef: "nudge", TargetRef: "send-nudge"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "approval-non-interrupting", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	h.dueNow(ctx, t, instanceID)
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if !h.waitingAt(ctx, t, instanceID, "send-nudge") {
		t.Error("the boundary event did not fire")
	}
	if !h.waitingAt(ctx, t, instanceID, "approve") {
		t.Error("a non-interrupting boundary event cancelled the activity it was watching")
	}
}

// The default stays interrupting, which is BPMN's default and what every stored
// definition already relies on.
func TestBoundaryEventInterruptsByDefault(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Interrupting Default Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "approval-interrupting",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "approve", Type: entities.UserTask, Name: "Approve the request"},
			{ID: "deadline", Type: entities.BoundaryEvent, AttachedToRef: "approve", Properties: map[string]any{
				"timer_duration": "PT2H",
			}},
			{ID: "escalate", Type: entities.UserTask, Name: "Escalate"},
			{ID: "done", Type: entities.UserTask, Name: "Record the decision"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "done"},
			{ID: "f3", SourceRef: "done", TargetRef: "end"},
			{ID: "f4", SourceRef: "deadline", TargetRef: "escalate"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "approval-interrupting", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	h.dueNow(ctx, t, instanceID)
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if !h.waitingAt(ctx, t, instanceID, "escalate") {
		t.Error("the deadline did not fire")
	}

	// The activity's token is what says whether it is still live. Its task row
	// is a separate question — see TestInterruptedActivityLeavesItsTaskOpen.
	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	if len(instance.GetTokensByNode(&entities.Node{ID: "approve"})) != 0 {
		t.Error("an interrupting boundary event left the activity running")
	}
}

// An interrupted activity's task must leave the inbox.
//
// Removing the token cancels the activity as far as the engine is concerned,
// but the task it created is a separate row. Nothing closed it, so the work
// stayed in whoever's inbox it was assigned to and completing it acted on an
// activity the process had already abandoned.
func TestInterruptedActivityCancelsItsTask(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Interrupted Task Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "approval-orphan-task",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "approve", Type: entities.UserTask, Name: "Approve the request", Assignee: "carol"},
			{ID: "deadline", Type: entities.BoundaryEvent, AttachedToRef: "approve", Properties: map[string]any{
				"timer_duration": "PT2H",
			}},
			{ID: "escalate", Type: entities.UserTask, Name: "Escalate"},
			{ID: "done", Type: entities.UserTask, Name: "Record the decision"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "done"},
			{ID: "f3", SourceRef: "done", TargetRef: "end"},
			{ID: "f4", SourceRef: "deadline", TargetRef: "escalate"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "approval-orphan-task", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	if !h.waitingAt(ctx, t, instanceID, "approve") {
		t.Fatal("the approval task was never offered")
	}

	// The deadline passes with the approval still open.
	h.dueNow(ctx, t, instanceID)
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	if len(instance.GetTokensByNode(&entities.Node{ID: "approve"})) != 0 {
		t.Fatal("the activity was not interrupted at all")
	}
	if h.waitingAt(ctx, t, instanceID, "approve") {
		t.Error("the interrupted activity is still offered in the inbox")
	}
	if !h.waitingAt(ctx, t, instanceID, "escalate") {
		t.Error("the escalation path did not open")
	}
}
