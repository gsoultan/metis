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

// A repeating timer needs the job to be re-enqueued after each firing, and the
// engine has no mechanism for that. Reading R3/PT10M as a one-shot timer would
// fire once and look like it worked.
func TestRepeatingTimerIsRefusedRatherThanFiringOnce(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Timer Cycle Project")

	def := timerDefinition("cooling-off-cycle", "R3/PT10M")
	def.Project = &entities.Project{ID: h.projID}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	_, err := h.svc.StartProcess(ctx, h.projID, "cooling-off-cycle", nil)
	if err == nil {
		t.Fatal("a repeating timer was accepted; it would have fired once and looked correct")
	}
	if !strings.Contains(err.Error(), "repeat") && !strings.Contains(err.Error(), "cycle") {
		t.Errorf("the error does not explain that repeating timers are unsupported: %v", err)
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
