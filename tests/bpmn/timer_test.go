package bpmn_test

import (
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/gobpm/server/domains/entities"
)

// A process that waits.
//
// The property panel tells the user in so many words: "PT1H is one hour, PT10M
// ten minutes, P1D a day". That is BPMN's own format and what every imported
// BPMN file contains. The engine read timers with Go's time.ParseDuration,
// which understands "1h30m" and none of the above — so a timer written exactly
// as the designer instructs was rejected outright.

func timerDefinition(key, timerValue string) entities.ProcessDefinition {
	return entities.ProcessDefinition{
		Key: key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "wait", Type: entities.IntermediateCatchEvent, Name: "Wait for the cooling-off period",
				Properties: map[string]any{"timer_duration": timerValue}},
			{ID: "ship", Type: entities.UserTask, Name: "Ship the order"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "wait"},
			{ID: "f2", SourceRef: "wait", TargetRef: "ship"},
			{ID: "f3", SourceRef: "ship", TargetRef: "end"},
		},
	}
}

// The ISO-8601 durations the designer documents must schedule a timer.
func TestTimerAcceptsTheDurationsTheDesignerDocuments(t *testing.T) {
	for _, timerValue := range []string{"PT1H", "PT10M", "P1D", "P1DT2H30M"} {
		t.Run(timerValue, func(t *testing.T) {
			ctx := t.Context()
			h := newEngineHarness(t, "Timer Project "+timerValue)

			def := timerDefinition("cooling-off-"+strings.ToLower(timerValue), timerValue)
			def.Project = &entities.Project{ID: h.projID}
			if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
				t.Fatalf("create definition: %v", err)
			}

			instanceID, err := h.svc.StartProcess(ctx, h.projID, def.Key, nil)
			if err != nil {
				t.Fatalf("a timer written the way the designer documents was rejected: %v", err)
			}

			// The process waits at the timer rather than running straight through.
			if h.waitingAt(ctx, t, instanceID, "ship") {
				t.Error("the process ran past its timer without waiting")
			}
		})
	}
}

// The timer has to be scheduled for the right moment, not merely accepted.
func TestTimerSchedulesTheJobAtTheRightTime(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Timer Schedule Project")

	def := timerDefinition("cooling-off-scheduled", "PT10M")
	def.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	before := time.Now()
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "cooling-off-scheduled", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	jobs, err := h.repo.Job().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected exactly one timer job, got %d", len(jobs))
	}

	// PT10M is ten minutes, not ten of anything else.
	wantEarliest := before.Add(10 * time.Minute)
	wantLatest := time.Now().Add(10*time.Minute + time.Minute)
	if jobs[0].NextRunAt.Before(wantEarliest) || jobs[0].NextRunAt.After(wantLatest) {
		t.Errorf("PT10M scheduled the timer for %v, expected around %v", jobs[0].NextRunAt, wantEarliest)
	}
}

// A repeating timer nags while the activity it watches is still open.
//
// "R3/PT10M" on a non-interrupting boundary event means: remind three times,
// ten minutes apart, without cancelling the work. Each occurrence is its own
// job, so firing one has to queue the next.
func TestRepeatingBoundaryTimerFiresItsFullCount(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Timer Cycle Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "approval-with-reminders",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "approve", Type: entities.UserTask, Name: "Approve the request"},
			// Non-interrupting: the approver keeps working while being reminded.
			{ID: "remind", Type: entities.BoundaryEvent, AttachedToRef: "approve",
				Properties: map[string]any{"timer_duration": "R3/PT10M", "non_interrupting": true}},
			{ID: "send-reminder", Type: entities.UserTask, Name: "Send a reminder"},
			{ID: "done", Type: entities.UserTask, Name: "Record the decision"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "done"},
			{ID: "f3", SourceRef: "done", TargetRef: "end"},
			{ID: "f4", SourceRef: "remind", TargetRef: "send-reminder"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "approval-with-reminders", nil)
	if err != nil {
		t.Fatalf("a repeating timer was rejected: %v", err)
	}

	// Nobody approves. Let each reminder come due in turn.
	for range 5 {
		if h.dueNow(ctx, t, instanceID) == 0 {
			break
		}
		if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
			t.Fatalf("process pending jobs: %v", err)
		}
	}

	if got := h.countTasksAt(ctx, t, instanceID, "send-reminder"); got != 3 {
		t.Errorf("expected 3 reminders from R3/PT10M, got %d", got)
	}
	// The work itself was never cancelled.
	if !h.waitingAt(ctx, t, instanceID, "approve") {
		t.Error("a non-interrupting reminder cancelled the activity it was watching")
	}
}

// A repeating timer stops once the activity it watches is done.
func TestRepeatingBoundaryTimerStopsWhenTheActivityCompletes(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Timer Cycle Stop Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "approval-reminders-stop",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "approve", Type: entities.UserTask, Name: "Approve the request"},
			{ID: "remind", Type: entities.BoundaryEvent, AttachedToRef: "approve",
				Properties: map[string]any{"timer_duration": "R/PT10M", "non_interrupting": true}},
			{ID: "send-reminder", Type: entities.UserTask, Name: "Send a reminder"},
			{ID: "done", Type: entities.UserTask, Name: "Record the decision"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "done"},
			{ID: "f3", SourceRef: "done", TargetRef: "end"},
			{ID: "f4", SourceRef: "remind", TargetRef: "send-reminder"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "approval-reminders-stop", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	// One reminder goes out, then the approver responds.
	h.dueNow(ctx, t, instanceID)
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}
	tasks, err := h.svc.ListTasks(ctx, h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == "approve" {
			if err := h.svc.CompleteTask(ctx, task.ID, "carol", nil); err != nil {
				t.Fatalf("approve: %v", err)
			}
		}
	}
	before := h.countTasksAt(ctx, t, instanceID, "send-reminder")

	// An unbounded cycle would nag forever; the activity is finished, so it must not.
	for range 3 {
		if h.dueNow(ctx, t, instanceID) == 0 {
			break
		}
		if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
			t.Fatalf("process pending jobs: %v", err)
		}
	}

	if after := h.countTasksAt(ctx, t, instanceID, "send-reminder"); after != before {
		t.Errorf("reminders kept going after the approval was done: %d then %d", before, after)
	}
}

// Definitions written against the previous behaviour keep working.
func TestTimerStillAcceptsGoStyleDurations(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Timer Legacy Project")

	def := timerDefinition("cooling-off-legacy", "1h30m")
	def.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	if _, err := h.svc.StartProcess(ctx, h.projID, "cooling-off-legacy", nil); err != nil {
		t.Errorf("a Go-style duration that used to work was rejected: %v", err)
	}
}
