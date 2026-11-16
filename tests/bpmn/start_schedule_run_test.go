package bpmn_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
)

// Scheduled starts.
//
// A start event that declares a schedule starts a run of the live version at
// each scheduled time: one run, however many replicas are looking, and one
// catch-up run after a time nothing was looking.

// nightlyReport has two ways in: someone starts it by hand, or its schedule
// comes round. Each leads to its own first step, so where a run began shows
// in where it waits.
func nightlyReport(projectID uuid.UUID, schedule map[string]any) *entities.ProcessDefinition {
	nodes := []*entities.Node{
		{ID: "byHand", Type: entities.StartEvent, Name: "Started by hand"},
		{ID: "ask", Type: entities.UserTask, Name: "Say which report"},
		{ID: "review", Type: entities.UserTask, Name: "Review the report"},
		{ID: "end", Type: entities.EndEvent},
	}
	flows := []*entities.SequenceFlow{
		{ID: "f1", SourceRef: "byHand", TargetRef: "ask"},
		{ID: "f2", SourceRef: "ask", TargetRef: "end"},
		{ID: "f3", SourceRef: "review", TargetRef: "end"},
	}
	if schedule != nil {
		nodes = append(nodes, &entities.Node{ID: "onSchedule", Type: entities.StartEvent, Name: "Every so often",
			Properties: map[string]any{entities.StartScheduleProperty: schedule}})
		flows = append(flows, &entities.SequenceFlow{ID: "f4", SourceRef: "onSchedule", TargetRef: "review"})
	}
	return &entities.ProcessDefinition{Project: &entities.Project{ID: projectID}, Key: "nightly-report", Nodes: nodes, Flows: flows}
}

// everyTenMinutes is a schedule whose next time is never more than ten
// minutes off, so a test can stand just past it without waiting.
func everyTenMinutes() map[string]any {
	return map[string]any{"every": entities.ScheduleEveryInterval, "interval_minutes": 10, "time_zone": "UTC"}
}

// worker is the context the schedule worker runs under: the job worker's.
func (h engineHarness) worker() context.Context { return entities.WithSystemContext(h.Ctx()) }

// runSchedules is one round of the worker as of at.
func (h engineHarness) runSchedules(t *testing.T, at time.Time) int {
	t.Helper()
	started, err := h.engine.StartDueSchedules(h.worker(), at)
	if err != nil {
		t.Fatalf("scheduled starts as of %s: %v", at.Format(time.RFC3339), err)
	}
	return started
}

func (h engineHarness) scheduleState(t *testing.T, key string) entities.StartScheduleState {
	t.Helper()
	state, err := h.repo.StartSchedule().Get(h.Ctx(), h.projID, key)
	if err != nil {
		t.Fatalf("read the schedule state of %s: %v", key, err)
	}
	return state
}

func TestAScheduledTimeStartsOneRunAtItsStartEvent(t *testing.T) {
	h := newEngineHarness(t, "Scheduled Start Project")
	h.deploy(t, nightlyReport(h.projID, everyTenMinutes()))

	// Deployed, and the first time has not come: nothing is owed yet.
	if got := h.runSchedules(t, time.Now()); got != 0 {
		t.Fatalf("a schedule whose first time has not come started %d runs", got)
	}

	due := time.Now().Add(11 * time.Minute)
	if got := h.runSchedules(t, due); got != 1 {
		t.Fatalf("a schedule whose time had come started %d runs, want 1", got)
	}
	started := h.instances(t)
	if len(started) != 1 {
		t.Fatalf("expected one instance, got %d", len(started))
	}
	id := uuid.UUID(started[0].ID)
	if h.waitingAt(h.Ctx(), t, id, "ask") || !h.waitingAt(h.Ctx(), t, id, "review") {
		t.Fatal("the scheduled run began at the manual start event, not at the scheduled one")
	}

	// The same moment asked about again owes nothing more.
	if got := h.runSchedules(t, due); got != 0 {
		t.Fatalf("asking again about a time already dealt with started %d runs", got)
	}
	if got := len(h.instances(t)); got != 1 {
		t.Fatalf("one scheduled time left %d instances", got)
	}
}

func TestAScheduleNobodyLookedAtForALongTimeCatchesUpWithOneRun(t *testing.T) {
	h := newEngineHarness(t, "Scheduled Catch-Up Project")
	h.deploy(t, nightlyReport(h.projID, everyTenMinutes()))

	// Two hours on: a dozen times have passed.
	late := time.Now().Add(2 * time.Hour)
	if got := h.runSchedules(t, late); got != 1 {
		t.Fatalf("a dozen missed times started %d runs, want one catch-up run", got)
	}
	// And the schedule carries on from there, not from where it stopped.
	if got := h.runSchedules(t, late.Add(11*time.Minute)); got != 1 {
		t.Fatalf("the time after the catch-up started %d runs, want 1", got)
	}
}

func TestReplicasLookingAtOnceStartOneRun(t *testing.T) {
	h := newEngineHarness(t, "Scheduled Replicas Project")
	h.deploy(t, nightlyReport(h.projID, everyTenMinutes()))
	due := time.Now().Add(11 * time.Minute)

	const replicas = 8
	var wg sync.WaitGroup
	counts := make([]int, replicas)
	errs := make([]error, replicas)
	for i := range replicas {
		wg.Go(func() {
			counts[i], errs[i] = h.engine.StartDueSchedules(h.worker(), due)
		})
	}
	wg.Wait()

	total := 0
	for i := range replicas {
		if errs[i] != nil {
			t.Fatalf("replica %d: %v", i, errs[i])
		}
		total += counts[i]
	}
	if total != 1 || len(h.instances(t)) != 1 {
		t.Fatalf("%d replicas reported %d runs and left %d instances for one scheduled time",
			replicas, total, len(h.instances(t)))
	}
}

// The live version dropped the schedule; an older one still has it.
func TestAScheduleOnlyAnOlderVersionHadStartsNothing(t *testing.T) {
	h := newEngineHarness(t, "Scheduled Retired Project")
	h.deploy(t, nightlyReport(h.projID, everyTenMinutes()))
	h.deploy(t, nightlyReport(h.projID, nil))

	if got := h.runSchedules(t, time.Now().Add(time.Hour)); got != 0 {
		t.Fatalf("a schedule only a retired version declared started %d runs", got)
	}
	if next := h.scheduleState(t, "nightly-report").NextLookAt; next != nil {
		t.Fatalf("a process with no schedule is still waiting to be looked at, at %s", next)
	}
}

// The schedule had not been looked at for an hour when a version that
// declares one went live. That hour was not the new version's.
func TestAVersionOwesNothingForTheTimeBeforeItWasLive(t *testing.T) {
	h := newEngineHarness(t, "Scheduled Live Since Project")
	h.deploy(t, nightlyReport(h.projID, nil))
	state := h.scheduleState(t, "nightly-report")
	anHourAgo := time.Now().Add(-time.Hour)
	if err := h.repo.StartSchedule().Settle(h.worker(), state.ID, anHourAgo, nil); err != nil {
		t.Fatalf("wind the watermark back: %v", err)
	}

	h.deploy(t, nightlyReport(h.projID, everyTenMinutes()))

	if got := h.runSchedules(t, time.Now()); got != 0 {
		t.Fatalf("a version just made live started %d runs for times before it was", got)
	}
	if got := h.runSchedules(t, time.Now().Add(11*time.Minute)); got != 1 {
		t.Fatalf("its first time of its own started %d runs, want 1", got)
	}
}

// A version arranged to take over later brings its schedule with it at that
// moment and not before.
func TestAScheduleFollowsAnArrangedCutover(t *testing.T) {
	h := newEngineHarness(t, "Scheduled Cutover Project")
	h.deploy(t, nightlyReport(h.projID, nil))
	if _, err := h.svc.DeployDefinition(h.Ctx(), nightlyReport(h.projID, everyTenMinutes()), false); err != nil {
		t.Fatalf("stage v2: %v", err)
	}
	cutover := time.Now().Add(3 * time.Second)
	if err := h.svc.ScheduleDefinitionVersion(h.Ctx(), h.projID, "nightly-report", 2, cutover); err != nil {
		t.Fatalf("arrange the cutover: %v", err)
	}

	if got := h.runSchedules(t, time.Now()); got != 0 {
		t.Fatalf("a schedule of a version not yet live started %d runs", got)
	}
	next := h.scheduleState(t, "nightly-report").NextLookAt
	if next == nil || next.Sub(cutover).Abs() > time.Millisecond {
		t.Fatalf("the process is to be looked at next at %v, want the cutover at %s", next, cutover)
	}

	time.Sleep(time.Until(cutover) + 200*time.Millisecond)
	if got := h.runSchedules(t, time.Now()); got != 0 {
		t.Fatalf("the cutover itself started %d runs", got)
	}
	if got := h.runSchedules(t, time.Now().Add(11*time.Minute)); got != 1 {
		t.Fatalf("the new live version's first time started %d runs, want 1", got)
	}
}

// A schedule that cannot be read fails its own process and nobody else's, and
// says so on its row.
func TestAScheduleThatFailsIsRecordedAndDoesNotStopTheOthers(t *testing.T) {
	h := newEngineHarness(t, "Scheduled Failure Project")
	h.deploy(t, nightlyReport(h.projID, everyTenMinutes()))

	// Written past the deploy check, as a version deployed before the check
	// existed could have been.
	broken := nightlyReport(h.projID, map[string]any{"every": "fortnight"})
	broken.Key = "broken-report"
	broken.ID = uuid.New()
	broken.Version = 1
	if err := h.repo.Definition().Create(h.Ctx(), adapters.DefinitionModelAdapter{Definition: broken}.ToModel()); err != nil {
		t.Fatalf("store the broken version: %v", err)
	}
	if err := h.repo.StartSchedule().Look(h.Ctx(), h.projID, broken.Key); err != nil {
		t.Fatalf("ask for the broken version's schedule to be looked at: %v", err)
	}

	due := time.Now().Add(11 * time.Minute)
	started, err := h.engine.StartDueSchedules(h.worker(), due)
	if err == nil || !strings.Contains(err.Error(), "broken-report") {
		t.Fatalf("a schedule that cannot be read was reported as %v", err)
	}
	if started != 1 {
		t.Fatalf("the healthy process started %d runs beside a broken one, want 1", started)
	}

	state := h.scheduleState(t, "broken-report")
	if state.Failures != 1 || state.LastError == "" {
		t.Fatalf("the failure was recorded as %d failures, %q", state.Failures, state.LastError)
	}
	if state.NextLookAt == nil || !state.NextLookAt.After(due) {
		t.Fatalf("the failed look is to be tried again at %v, want after %s", state.NextLookAt, due)
	}
}
