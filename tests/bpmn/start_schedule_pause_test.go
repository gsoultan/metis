package bpmn_test

import (
	"errors"
	"testing"
	"time"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/store/processdefinitionrelease"
)

// Pausing scheduled starts, and saying when the next run is due.
//
// A pause belongs to the process. While it holds, scheduled times pass and
// start nothing, and resuming starts nothing for them either.

func (h engineHarness) schedule(t *testing.T, key string) entities.StartScheduleStatus {
	t.Helper()
	status, err := h.svc.GetStartSchedule(h.Ctx(), h.projID, key)
	if err != nil {
		t.Fatalf("read the schedule of %s: %v", key, err)
	}
	return status
}

func (h engineHarness) pauseSchedule(t *testing.T, key string, paused bool) entities.StartScheduleStatus {
	t.Helper()
	status, err := h.svc.SetStartSchedulePaused(h.Ctx(), h.projID, key, paused)
	if err != nil {
		t.Fatalf("set the schedule of %s paused=%v: %v", key, paused, err)
	}
	return status
}

func TestAScheduledProcessSaysWhenItsNextRunIs(t *testing.T) {
	h := newEngineHarness(t, "Scheduled Next Run Project")
	h.deploy(t, nightlyReport(h.projID, everyTenMinutes()))

	status := h.schedule(t, "nightly-report")
	if !status.Scheduled || status.Paused {
		t.Fatalf("a scheduled process reads scheduled=%v paused=%v", status.Scheduled, status.Paused)
	}
	if status.NextRunAt == nil {
		t.Fatal("a scheduled process has no next run")
	}
	if wait := time.Until(*status.NextRunAt); wait <= 0 || wait > 10*time.Minute {
		t.Fatalf("the next run of a ten-minute schedule is %s away", wait)
	}
}

func TestAProcessWithNoScheduleSaysSoAndCannotBePaused(t *testing.T) {
	h := newEngineHarness(t, "Unscheduled Project")
	h.deploy(t, nightlyReport(h.projID, nil))

	status := h.schedule(t, "nightly-report")
	if status.Scheduled || status.Paused || status.NextRunAt != nil {
		t.Fatalf("a process with no schedule reads %+v", status)
	}
	if _, err := h.svc.SetStartSchedulePaused(h.Ctx(), h.projID, "nightly-report", true); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("pausing a process with no schedule: got %v; want it refused", err)
	}
	// Resuming what was never paused is not an error: it is already running.
	if status := h.pauseSchedule(t, "nightly-report", false); status.Paused {
		t.Fatal("resuming a process with no schedule left it paused")
	}
	if _, err := h.svc.GetStartSchedule(h.Ctx(), h.projID, "no-such-process"); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("the schedule of a process that does not exist: got %v; want not found", err)
	}
}

func TestAPausedScheduleStartsNothingAndResumingOwesNothingForThePause(t *testing.T) {
	h := newEngineHarness(t, "Scheduled Pause Project")
	h.deploy(t, nightlyReport(h.projID, everyTenMinutes()))

	paused := h.pauseSchedule(t, "nightly-report", true)
	if !paused.Paused || paused.PausedAt == nil || paused.NextRunAt != nil {
		t.Fatalf("a paused schedule reads %+v", paused)
	}
	// Pausing again keeps the first pause.
	if again := h.pauseSchedule(t, "nightly-report", true); !again.PausedAt.Equal(*paused.PausedAt) {
		t.Fatalf("pausing twice moved the pause from %s to %s", paused.PausedAt, again.PausedAt)
	}

	// Its time comes and goes.
	due := time.Now().Add(11 * time.Minute)
	if got := h.runSchedules(t, due); got != 0 {
		t.Fatalf("a paused schedule started %d runs", got)
	}
	if got := len(h.instances(t)); got != 0 {
		t.Fatalf("a paused schedule left %d instances", got)
	}

	resumed := h.pauseSchedule(t, "nightly-report", false)
	if resumed.Paused || resumed.PausedAt != nil || resumed.NextRunAt == nil {
		t.Fatalf("a resumed schedule reads %+v", resumed)
	}
	// The time that passed while paused is not made up for.
	if got := h.runSchedules(t, due.Add(time.Minute)); got != 0 {
		t.Fatalf("resuming started %d runs for a time that passed while paused", got)
	}
	// And the one after it starts as usual.
	if got := h.runSchedules(t, due.Add(11*time.Minute)); got != 1 {
		t.Fatalf("the first time after resuming started %d runs, want 1", got)
	}
}

// liveAndUnlookedSince rewrites history so that the process has been live, and last
// looked at, since at: what an installation whose worker stopped then looks
// like now.
func (h engineHarness) liveAndUnlookedSince(t *testing.T, key string, at time.Time) {
	t.Helper()
	// The release is what says since when a version has been live.
	var released processdefinitionrelease.Mut
	released.SetActivateAt(at)
	if n, err := processdefinitionrelease.New().
		Where(processdefinitionrelease.ProjectID.Eq(h.projID), processdefinitionrelease.ProcessKey.Eq(key)).
		UpdateAll(h.Ctx(), h.ex(), released); err != nil || n != 1 {
		t.Fatalf("backdate the release of %s: %d row(s), %v", key, n, err)
	}
	overdue := at.Add(10 * time.Minute)
	if err := h.repo.StartSchedule().Settle(h.worker(), h.scheduleState(t, key).ID, at, &overdue); err != nil {
		t.Fatalf("set the watermark of %s back: %v", key, err)
	}
}

// A pause with no worker running to pass the times over: the watermark is
// still where it was when the worker stopped, and resuming must not read
// that as a run owed.
func TestResumingAfterNoWorkerRanStartsNoCatchUpRun(t *testing.T) {
	h := newEngineHarness(t, "Scheduled Pause Outage Project")
	h.deploy(t, nightlyReport(h.projID, everyTenMinutes()))
	h.pauseSchedule(t, "nightly-report", true)
	h.liveAndUnlookedSince(t, "nightly-report", time.Now().Add(-time.Hour))

	h.pauseSchedule(t, "nightly-report", false)
	if got := h.runSchedules(t, time.Now()); got != 0 {
		t.Fatalf("resuming after an outage started %d catch-up runs", got)
	}
}

// Resuming a schedule that is already running moves nothing: a run that is
// owed stays owed.
func TestResumingARunningScheduleKeepsTheRunItOwes(t *testing.T) {
	h := newEngineHarness(t, "Scheduled Resume Running Project")
	h.deploy(t, nightlyReport(h.projID, everyTenMinutes()))
	h.liveAndUnlookedSince(t, "nightly-report", time.Now().Add(-time.Hour))

	h.pauseSchedule(t, "nightly-report", false)
	if got := h.runSchedules(t, time.Now()); got != 1 {
		t.Fatalf("resuming a running schedule left %d runs owed, want the one catch-up run", got)
	}
}

func TestAPauseHoldsAcrossADeploy(t *testing.T) {
	h := newEngineHarness(t, "Scheduled Pause Deploy Project")
	h.deploy(t, nightlyReport(h.projID, everyTenMinutes()))
	h.pauseSchedule(t, "nightly-report", true)

	h.deploy(t, nightlyReport(h.projID, everyTenMinutes()))
	if !h.schedule(t, "nightly-report").Paused {
		t.Fatal("deploying a new version resumed the schedule")
	}
	if got := h.runSchedules(t, time.Now().Add(11*time.Minute)); got != 0 {
		t.Fatalf("a paused schedule started %d runs after a deploy", got)
	}
}

// The next run is the first scheduled time that falls while its own version
// is live, so a schedule that only arrives with an arranged version is
// counted from the cutover.
func TestTheNextRunFollowsAnArrangedCutover(t *testing.T) {
	h := newEngineHarness(t, "Scheduled Next Run Cutover Project")
	h.deploy(t, nightlyReport(h.projID, nil))
	h.stage(t, nightlyReport(h.projID, everyTenMinutes()))
	cutover := time.Now().Add(time.Hour)
	if err := h.svc.ScheduleDefinitionVersion(h.Ctx(), h.projID, "nightly-report", 2, cutover); err != nil {
		t.Fatalf("arrange the cutover: %v", err)
	}

	status := h.schedule(t, "nightly-report")
	if !status.Scheduled || status.NextRunAt == nil {
		t.Fatalf("a process whose arranged version has a schedule reads %+v", status)
	}
	if wait := status.NextRunAt.Sub(cutover); wait <= 0 || wait > 10*time.Minute {
		t.Fatalf("the next run is %s after the cutover, want within the ten minutes after it", wait)
	}
}

func TestAnotherOrganizationCannotReadOrPauseASchedule(t *testing.T) {
	h := newEngineHarness(t, "Scheduled Pause Tenant Project")
	h.deploy(t, nightlyReport(h.projID, everyTenMinutes()))

	other, err := h.svc.CreateOrganization(t.Context(), "Another Schedule Org", "")
	if err != nil {
		t.Fatalf("create another organization: %v", err)
	}
	outsider := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: other.ID.String()})
	if _, err := h.svc.CreateProject(outsider, other.ID, "Another Project", ""); err != nil {
		t.Fatalf("create another organization's project: %v", err)
	}

	if _, err := h.svc.GetStartSchedule(outsider, h.projID, "nightly-report"); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("another organization reading the schedule: got %v; want not found", err)
	}
	if _, err := h.svc.SetStartSchedulePaused(outsider, h.projID, "nightly-report", true); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("another organization pausing the schedule: got %v; want not found", err)
	}
	if h.schedule(t, "nightly-report").Paused {
		t.Fatal("another organization paused the schedule")
	}
}
