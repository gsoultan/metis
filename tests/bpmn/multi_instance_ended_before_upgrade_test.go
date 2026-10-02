package bpmn_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// A step whose completion condition ended it before the upgrade, with work
// still in flight for the runs nobody needed: a process it called, work parked
// for a worker, a call still queued.
//
// The release before migration 31 dropped the step's count when it ended and
// left a token on it for every run. Whether a step was still waiting was then
// asked three ways — "is it counting" by the engine, "is there a token on it"
// by everything that brings work back — and for this state they disagree: the
// work was let in, because there was a token, and then refused, because there
// was no count. For ever, since the refusal rolled back whatever brought it.
//
// The question is now asked one way (ProcessInstance.WaitsFor), and tokens on
// a step that is not counting are nobody waiting.

// leftBehindByTheOldRelease puts back on a step what the release before left
// there when the step ended early: a token for each run, and no count.
func leftBehindByTheOldRelease(ctx context.Context, t *testing.T, h engineHarness, instanceID uuid.UUID, nodeID string, runs ...string) {
	t.Helper()
	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	if instance.IsMultiInstanceActive(nodeID) {
		t.Fatalf("staging failed: the step is still counting its runs, so it has not ended")
	}
	for _, run := range runs {
		instance.AddTokenWithIteration(&entities.Node{ID: nodeID}, run)
	}
	if err := h.engine.UpdateInstance(ctx, instance); err != nil {
		t.Fatalf("stage the tokens the old release left: %v", err)
	}
	if held := tokenIterationsOn(ctx, t, h, instanceID, nodeID); len(held) != len(runs) {
		t.Fatalf("staging failed: the step holds tokens %v, want %v", held, runs)
	}
}

// trailLines returns the instance's trail entries of one type.
func trailLines(ctx context.Context, t *testing.T, h engineHarness, instanceID uuid.UUID, entryType string) []entities.AuditEntry {
	t.Helper()
	trail, err := h.engine.GetAuditLogs(ctx, instanceID)
	if err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	var lines []entities.AuditEntry
	for _, entry := range trail {
		if entry.Type == entryType {
			lines = append(lines, entry)
		}
	}
	return lines
}

// openIncidents returns the instance's open incidents.
func openIncidents(ctx context.Context, t *testing.T, h engineHarness, instanceID uuid.UUID) []models.IncidentModel {
	t.Helper()
	incidents, err := h.repo.Incident().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("read the incidents: %v", err)
	}
	var open []models.IncidentModel
	for _, incident := range incidents {
		if incident.Status == models.IncidentOpen {
			open = append(open, incident)
		}
	}
	return open
}

// BPMN 2.0.2 §10.3.8: a completionCondition that holds cancels the remaining
// activity instances and produces a token — one.
//
// The third called process was still running at the upgrade. When it finished,
// its parent held a token on the step that had called it, so the parent was
// resumed; the step was not counting, so the resume was refused; and the
// refusal rolled back the completion that ended the called process, which
// could then never end.
func TestACalledProcessReturningToAStepThatEndedBeforeTheUpgradeCanStillEnd(t *testing.T) {
	h := newEngineHarness(t, "Called Before Upgrade Project")
	ctx := h.Ctx()
	deploySupplierCheck(t, h)
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "two-of-three-suppliers-upgraded",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "check", Type: entities.CallActivity, Name: "Check the supplier",
				MultiInstanceType: "parallel", Collection: "suppliers", ElementVariable: "supplier",
				CompletionCondition: "nrOfCompletedInstances >= 2",
				Properties:          map[string]any{"called_process_key": "supplier-check"}},
			{ID: "record", Type: entities.UserTask, Name: "Record the outcome"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "check"},
			{ID: "f2", SourceRef: "check", TargetRef: "record"},
			{ID: "f3", SourceRef: "record", TargetRef: "end"},
		},
	})
	parentID, err := h.svc.StartProcess(ctx, h.projID, "two-of-three-suppliers-upgraded", map[string]any{
		"suppliers": []any{"northwind", "contoso", "fabrikam"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	called := calledBy(ctx, t, h, parentID)
	if len(called) != 3 {
		t.Fatalf("three suppliers were to be checked and %d process(es) were called", len(called))
	}
	for _, childID := range called[:2] {
		if err := finishCalled(ctx, t, h, childID, true); err != nil {
			t.Fatalf("finish a called process: %v", err)
		}
	}
	if seen := tasksEverOn(t, h, parentID, "record"); seen != 1 {
		t.Fatalf("after the second check the process moved on %d times, want once", seen)
	}
	leftBehindByTheOldRelease(ctx, t, h, parentID, "check", "0", "1", "2")

	late := called[2]
	if err := finishCalled(ctx, t, h, late, false); err != nil {
		t.Fatalf("the called process could not end: %v", err)
	}
	requireInstanceStatus(ctx, t, h, late, entities.ProcessCompleted)
	if seen := tasksEverOn(t, h, parentID, "record"); seen != 1 {
		t.Fatalf("a called process finishing late moved its parent on again: %d times", seen)
	}
	parent, err := h.engine.GetInstance(ctx, parentID)
	if err != nil {
		t.Fatalf("reload the parent: %v", err)
	}
	if parent.Variables["approved"] != true {
		t.Errorf("the late result was written over the parent's: approved = %v", parent.Variables["approved"])
	}
	requireLateReturnRecorded(ctx, t, h, parentID, "Check the supplier")
}

// twoOfThreeChecksEndedBeforeTheUpgrade starts three checks parked for
// workers, has two of them done — which ends the step — and then puts back
// what the old release left: a token for every run, and the third check still
// parked. It returns the instance.
func twoOfThreeChecksEndedBeforeTheUpgrade(t *testing.T, h engineHarness, key string) uuid.UUID {
	t.Helper()
	ctx := h.Ctx()
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "check", Type: entities.ServiceTask, Name: "Check the supplier", ExternalTopic: key,
				MultiInstanceType: "parallel", Collection: "suppliers", ElementVariable: "supplier",
				CompletionCondition: "nrOfCompletedInstances >= 2"},
			{ID: "record", Type: entities.UserTask, Name: "Record the outcome"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "check"},
			{ID: "f2", SourceRef: "check", TargetRef: "record"},
			{ID: "f3", SourceRef: "record", TargetRef: "end"},
		},
	})
	instanceID, err := h.svc.StartProcess(ctx, h.projID, key, map[string]any{
		"suppliers": []any{"northwind", "contoso", "fabrikam"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	fetched, err := h.svc.FetchAndLock(ctx, key, "worker", 2, 60_000)
	if err != nil || len(fetched) != 2 {
		t.Fatalf("fetch two checks: %d, %v", len(fetched), err)
	}
	for _, task := range fetched {
		if err := h.svc.Complete(ctx, task.ID, "worker", nil); err != nil {
			t.Fatalf("complete a check: %v", err)
		}
	}
	if seen := tasksEverOn(t, h, instanceID, "record"); seen != 1 {
		t.Fatalf("after the second check the process moved on %d times, want once", seen)
	}

	// This release withdrew the third check as the step ended. The one before
	// left it parked, so it is put back.
	if err := h.db.Exec(`UPDATE external_tasks SET deleted_at = NULL
		 WHERE instance_id = ? AND node_id = ? AND id NOT IN (?, ?)`,
		instanceID, "check", fetched[0].ID, fetched[1].ID).Error; err != nil {
		t.Fatalf("park the third check again: %v", err)
	}
	if parked := parkedFor(t, h, instanceID, "check"); parked != 1 {
		t.Fatalf("staging failed: %d check(s) are parked, want the one the old release left", parked)
	}
	leftBehindByTheOldRelease(ctx, t, h, instanceID, "check", "0", "1", "2")
	return instanceID
}

// requireNoSuchTask fails unless a worker's report was answered the way a
// report on withdrawn work is: there is no such task.
func requireNoSuchTask(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("a report on work for a step that had ended was answered %v, want no such task", err)
	}
}

// requireCheckWithdrawn fails unless the work left parked for the ended step
// is gone, is offered to nobody, and the trail says it was withdrawn.
func requireCheckWithdrawn(ctx context.Context, t *testing.T, h engineHarness, instanceID uuid.UUID, topic string, linesBefore int) {
	t.Helper()
	if parked := parkedFor(t, h, instanceID, "check"); parked != 0 {
		t.Fatalf("%d check(s) are still parked for a step that ended", parked)
	}
	offered, err := h.svc.FetchAndLock(ctx, topic, "another-worker", 10, 60_000)
	if err != nil {
		t.Fatalf("fetch again: %v", err)
	}
	if len(offered) != 0 {
		t.Fatalf("a worker is still offered %d check(s) for a step that ended", len(offered))
	}
	lines := trailLines(ctx, t, h, instanceID, "parked_work_withdrawn")
	if len(lines) != linesBefore+1 {
		t.Fatalf("the trail has %d line(s) saying parked work was withdrawn, want %d", len(lines), linesBefore+1)
	}
	line := lines[len(lines)-1]
	if !strings.Contains(line.Narrative, "Check the supplier") || !strings.Contains(line.Narrative, "withdrawn") {
		t.Fatalf("the trail's line does not say what was withdrawn from which step: %q", line.Narrative)
	}
	if seen := tasksEverOn(t, h, instanceID, "record"); seen != 1 {
		t.Fatalf("work for a step that had ended moved the process on again: %d times", seen)
	}
	if open := openIncidents(ctx, t, h, instanceID); len(open) != 0 {
		t.Fatalf("work for a step that had ended raised %d incident(s): %+v", len(open), open)
	}
}

// BPMN 2.0.2 §10.3.8: the remaining activity instances are cancelled.
//
// The third check was still parked at the upgrade. A worker fetched it, did
// it, and was refused — the step had finished — and the refusal rolled back,
// so the lock ran out and the check was offered again, and refused again, for
// as long as the instance existed. It is withdrawn instead, the first time a
// worker reports on it, with a line on the trail.
func TestWorkParkedForAStepThatEndedBeforeTheUpgradeIsWithdrawnNotRefused(t *testing.T) {
	h := newEngineHarness(t, "Parked Before Upgrade Project")
	ctx := h.Ctx()
	const topic = "checks-ended-before-upgrade"
	instanceID := twoOfThreeChecksEndedBeforeTheUpgrade(t, h, topic)
	before := len(trailLines(ctx, t, h, instanceID, "parked_work_withdrawn"))

	leftover, err := h.svc.FetchAndLock(ctx, topic, "worker", 10, 60_000)
	if err != nil || len(leftover) != 1 {
		t.Fatalf("fetch the check the old release left: %d, %v", len(leftover), err)
	}
	requireNoSuchTask(t, h.svc.Complete(ctx, leftover[0].ID, "worker", map[string]any{"approved": false}))

	requireCheckWithdrawn(ctx, t, h, instanceID, topic, before)
	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	if _, set := instance.Variables["approved"]; set {
		t.Errorf("the result of work nobody was waiting for was written to the process: %v", instance.Variables)
	}
}

// The same leftover, when the worker fails at it and gives up. The failure was
// raised as an incident on a step that had finished, whose resolution offered
// the work again.
func TestAWorkerGivingUpOnAStepThatEndedBeforeTheUpgradeRaisesNothing(t *testing.T) {
	h := newEngineHarness(t, "Failed Before Upgrade Project")
	ctx := h.Ctx()
	const topic = "checks-failed-after-upgrade"
	instanceID := twoOfThreeChecksEndedBeforeTheUpgrade(t, h, topic)
	before := len(trailLines(ctx, t, h, instanceID, "parked_work_withdrawn"))

	leftover, err := h.svc.FetchAndLock(ctx, topic, "worker", 10, 60_000)
	if err != nil || len(leftover) != 1 {
		t.Fatalf("fetch the check the old release left: %d, %v", len(leftover), err)
	}
	requireNoSuchTask(t, h.svc.HandleFailure(ctx, leftover[0].ID, "worker", "supplier unreachable", "", 0, 0))

	requireCheckWithdrawn(ctx, t, h, instanceID, topic, before)
}

// firstQuoteWins is start → ask each supplier for a quote (a call, the first
// answer ends the step) → record → end. With an error path, a failure of the
// call goes to somebody to handle.
func firstQuoteWins(projID uuid.UUID, key, url string, withErrorPath bool) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: projID},
		Key:     key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "quote", Type: entities.ServiceTask, Name: "Ask for a quote",
				MultiInstanceType: "parallel", Collection: "suppliers", ElementVariable: "supplier",
				CompletionCondition: "nrOfCompletedInstances >= 1",
				Properties:          map[string]any{"http_url": url, "http_method": "POST"}},
			{ID: "record", Type: entities.UserTask, Name: "Record the outcome"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "quote"},
			{ID: "f2", SourceRef: "quote", TargetRef: "record"},
			{ID: "f3", SourceRef: "record", TargetRef: "end"},
		},
	}
	if withErrorPath {
		def.Nodes = append(def.Nodes,
			&entities.Node{ID: "quote-failed", Type: entities.BoundaryEvent, AttachedToRef: "quote", CancelActivity: true,
				Properties: map[string]any{"event_type": "error"}},
			&entities.Node{ID: "handle", Type: entities.UserTask, Name: "Chase the supplier"},
		)
		def.Flows = append(def.Flows,
			&entities.SequenceFlow{ID: "e1", SourceRef: "quote-failed", TargetRef: "handle"},
			&entities.SequenceFlow{ID: "e2", SourceRef: "handle", TargetRef: "end"},
		)
	}
	return def
}

// firstQuoteEndedBeforeTheUpgrade starts three quotes, lets the first come in
// — which ends the step — and puts back what the old release left: a token for
// every run. The calls for the other two suppliers are still queued, as they
// were under either release. It returns the instance.
func firstQuoteEndedBeforeTheUpgrade(t *testing.T, h engineHarness, def *entities.ProcessDefinition) uuid.UUID {
	t.Helper()
	ctx := h.Ctx()
	h.deploy(t, def)
	instanceID, err := h.svc.StartProcess(ctx, h.projID, def.Key, map[string]any{
		"suppliers": []any{"northwind", "contoso", "fabrikam"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	queued := jobsOn(ctx, t, h, instanceID, "quote")
	if len(queued) != 3 {
		t.Fatalf("three quotes were asked for and %d call(s) are queued", len(queued))
	}
	for _, later := range queued[1:] {
		later.NextRunAt = time.Now().Add(time.Hour)
		if err := h.repo.Job().Update(ctx, later); err != nil {
			t.Fatalf("hold a call back: %v", err)
		}
	}
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}
	if seen := tasksEverOn(t, h, instanceID, "record"); seen != 1 {
		t.Fatalf("after the first quote the process moved on %d times, want once", seen)
	}
	leftBehindByTheOldRelease(ctx, t, h, instanceID, "quote", "0", "1", "2")
	if moved := h.dueNow(ctx, t, instanceID); moved != 2 {
		t.Fatalf("%d held-back call(s) came due, want 2", moved)
	}
	return instanceID
}

// BPMN 2.0.2 §10.3.8: the remaining activity instances are cancelled, and a
// cancelled call is not made.
//
// The calls for the other two suppliers were still queued at the upgrade. Each
// found a token on the step and made its call; the result was refused, since
// the step was not counting; the job failed, was retried — calling again each
// time — and ended as an incident on a step that had finished.
func TestACallQueuedForAStepThatEndedBeforeTheUpgradeIsNotMade(t *testing.T) {
	api, calls := partnerAPI(t, answersOK)
	h := newEngineHarness(t, "Queued Before Upgrade Project")
	ctx := h.Ctx()
	instanceID := firstQuoteEndedBeforeTheUpgrade(t, h,
		firstQuoteWins(h.projID, "first-quote-wins-upgraded", api.URL, false))

	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if made := calls.Load(); made != 1 {
		t.Fatalf("%d call(s) were made in all; the two runs the step ended without should have made none", made)
	}
	for _, job := range jobsOn(ctx, t, h, instanceID, "quote") {
		if job.Status != models.JobCompleted || job.Retries != 0 {
			t.Fatalf("the job for run %q is %s after %d failed attempt(s), want completed and none",
				job.IterationID, job.Status, job.Retries)
		}
	}
	if seen := tasksEverOn(t, h, instanceID, "record"); seen != 1 {
		t.Fatalf("a call for a run ended early moved the process on again: %d times", seen)
	}
	if open := openIncidents(ctx, t, h, instanceID); len(open) != 0 {
		t.Fatalf("a call for a step that had ended raised %d incident(s): %+v", len(open), open)
	}
}

// BPMN 2.0.2 §10.4.4 (Error Events): an error boundary event catches an error
// thrown by the activity it is attached to.
//
// The engine declining a result for a step that has finished is not that. With
// an error path on the step, the refusal was caught by it: the step's tokens
// were taken and the process went down the error path, from a step it had
// already left by the ordinary one.
func TestAnErrorPathDoesNotCatchAStepThatEndedBeforeTheUpgrade(t *testing.T) {
	api, calls := partnerAPI(t, answersOK)
	h := newEngineHarness(t, "Error Path Before Upgrade Project")
	ctx := h.Ctx()
	instanceID := firstQuoteEndedBeforeTheUpgrade(t, h,
		firstQuoteWins(h.projID, "first-quote-wins-error-path", api.URL, true))

	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if seen := tasksEverOn(t, h, instanceID, "handle"); seen != 0 {
		t.Fatalf("the error path was taken %d time(s) for a step that had finished", seen)
	}
	if made := calls.Load(); made != 1 {
		t.Fatalf("%d call(s) were made in all, want the one that ended the step", made)
	}
	if seen := tasksEverOn(t, h, instanceID, "record"); seen != 1 {
		t.Fatalf("the process moved past the step %d times, want once", seen)
	}
}

// BPMN 2.0.2 §10.4.4: an error boundary event is for an error of its activity.
//
// A call inside a sub-process whose run was ended from outside it. The call is
// made and its step finishes; the end event inside then finds the sub-process
// over, and the engine declines. That is a statement about the process, not a
// failure of the call — but it reached the job as an error, and an error path
// on the call caught it: the process carried on inside a sub-process it had
// left. It is recorded against the job instead, where an operator sees it.
func TestAnErrorPathDoesNotCatchTheEngineDecliningACompletion(t *testing.T) {
	api, calls := partnerAPI(t, answersOK)
	h := newEngineHarness(t, "Declined Completion Project")
	ctx := h.Ctx()
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "notify-each-supplier",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "sub", Type: entities.SubProcess, Name: "Tell each supplier",
				MultiInstanceType: "parallel", Collection: "suppliers", ElementVariable: "supplier"},
			{ID: "s_start", Type: entities.StartEvent, ParentID: "sub"},
			{ID: "notify", Type: entities.ServiceTask, Name: "Notify the supplier", ParentID: "sub",
				Properties: map[string]any{"http_url": api.URL, "http_method": "POST"}},
			{ID: "notify-failed", Type: entities.BoundaryEvent, AttachedToRef: "notify", ParentID: "sub",
				CancelActivity: true, Properties: map[string]any{"event_type": "error"}},
			{ID: "handle", Type: entities.UserTask, Name: "Chase the supplier", ParentID: "sub"},
			{ID: "s_end", Type: entities.EndEvent, ParentID: "sub"},
			{ID: "record", Type: entities.UserTask, Name: "Record the outcome"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "sub"},
			{ID: "i1", SourceRef: "s_start", TargetRef: "notify"},
			{ID: "i2", SourceRef: "notify", TargetRef: "s_end"},
			{ID: "i3", SourceRef: "notify-failed", TargetRef: "handle"},
			{ID: "i4", SourceRef: "handle", TargetRef: "s_end"},
			{ID: "f2", SourceRef: "sub", TargetRef: "record"},
			{ID: "f3", SourceRef: "record", TargetRef: "end"},
		},
	})
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "notify-each-supplier", map[string]any{
		"suppliers": []any{"northwind"},
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if queued := jobsOn(ctx, t, h, instanceID, "notify"); len(queued) != 1 {
		t.Fatalf("one supplier was to be told and %d call(s) are queued", len(queued))
	}

	// The sub-process's run is ended from outside it, which takes its count
	// and its tokens and leaves what is inside.
	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	instance.FinishMultiInstance("sub")
	instance.RemoveTokenByNodeID("sub")
	if err := h.engine.UpdateInstance(ctx, instance); err != nil {
		t.Fatalf("end the sub-process from outside: %v", err)
	}

	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if made := calls.Load(); made != 1 {
		t.Fatalf("the supplier was told %d time(s), want once: the step itself was still waiting", made)
	}
	if seen := tasksEverOn(t, h, instanceID, "handle"); seen != 0 {
		t.Fatalf("the error path caught the engine declining a completion: taken %d time(s)", seen)
	}
	job := jobsOn(ctx, t, h, instanceID, "notify")[0]
	if job.Status != models.JobPending || job.Retries != 1 {
		t.Fatalf("the job is %s with %d failure(s) counted, want pending with the one refusal recorded", job.Status, job.Retries)
	}
	if !strings.Contains(job.LastError, "already finished") {
		t.Fatalf("the job does not say why it was declined: %q", job.LastError)
	}
}
