package bpmn_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// A service task's call is a job, and a job outlives the step it was queued
// for: nothing takes it off the queue when the step is withdrawn. So the job
// has to ask, when it runs, whether anybody is still waiting for it.
//
// It asked only after making the call. A step withdrawn before its job ran was
// called anyway — a payment taken, a supplier notified, for a step the process
// had abandoned — and a call that failed was retried and then raised as an
// incident on an instance that had moved on.

// partnerAPI is a stub endpoint that counts the calls it receives. A service
// task's URL is user-authored, so the client refuses loopback unless told
// otherwise; the stub is on 127.0.0.1 and the test opts in for itself.
func partnerAPI(t *testing.T, respond http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	calls := &atomic.Int32{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		respond(w, r)
	}))
	t.Cleanup(api.Close)
	return api, calls
}

func answersOK(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// jobsOn returns the instance's jobs for one node, as stored.
func jobsOn(ctx context.Context, t *testing.T, h engineHarness, instanceID uuid.UUID, nodeID string) []models.JobModel {
	t.Helper()
	all, err := h.repo.Job().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	var mine []models.JobModel
	for _, job := range all {
		if job.NodeID == nodeID {
			mine = append(mine, job)
		}
	}
	return mine
}

// withANotification adds one more step to the ad-hoc research sub-process: a
// call to the insurer.
func withANotification(def *entities.ProcessDefinition, url string) {
	research := def.Nodes[1]
	research.Nodes = append(research.Nodes, &entities.Node{
		ID: "notify-insurer", Type: entities.ServiceTask, Name: "Notify the insurer", ParentID: "research",
		Properties: map[string]any{"http_url": url, "http_method": "POST"},
	})
}

// BPMN 2.0.2 §10.3.5, §13.2.5: when the completionCondition becomes true the
// instances still running inside are cancelled. A cancelled service task does
// not make its call.
func TestAServiceTaskWithdrawnWithItsAdHocSubProcessIsNotCalled(t *testing.T) {
	api, calls := partnerAPI(t, answersOK)
	h := newEngineHarness(t, "AdHoc Withdrawn Call Project")
	ctx := h.Ctx()

	def := adHocDefinition("claim-research-call", "reviewsDone >= 1")
	def.Project = &entities.Project{ID: h.projID}
	withANotification(&def, api.URL)
	h.deploy(t, &def)
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "claim-research-call", map[string]any{"reviewsDone": 0})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	for _, step := range []string{"call-customer", "notify-insurer"} {
		if err := h.svc.ActivateTask(ctx, instanceID, "research", step); err != nil {
			t.Fatalf("activate %s: %v", step, err)
		}
	}
	if queued := jobsOn(ctx, t, h, instanceID, "notify-insurer"); len(queued) != 1 || queued[0].Status != models.JobPending {
		t.Fatalf("the notification should be queued once and waiting: %+v", queued)
	}

	// The sub-process finishes before the worker gets to the notification.
	completeTaskAt(ctx, t, h, instanceID, "call-customer", map[string]any{"reviewsDone": 1})
	if !h.waitingAt(ctx, t, instanceID, "decide") {
		t.Fatal("the completion condition was met and the process did not carry on")
	}

	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if made := calls.Load(); made != 0 {
		t.Fatalf("the insurer was notified %d time(s) for a step that had been withdrawn", made)
	}
	job := jobsOn(ctx, t, h, instanceID, "notify-insurer")[0]
	if job.Status != models.JobCompleted || job.Retries != 0 {
		t.Fatalf("the withdrawn step's job is %s after %d attempt(s), want completed having made none", job.Status, job.Retries)
	}
	if seen := tasksEverOn(t, h, instanceID, "decide"); seen != 1 {
		t.Fatalf("the process moved past the sub-process %d times, want once", seen)
	}
}

// BPMN 2.0.2 §10.3.5, §13.2.5: a cancelled instance of a step is over, and a
// failure it has afterwards is nobody's to handle.
//
// Here the step is withdrawn while its call is in flight — the one moment a
// check before the call cannot cover — and the call then fails. The failure
// was counted against the job, and with its attempts used up raised as an
// incident on an instance that had moved on: something for an operator to
// resolve, whose resolution retried a call nobody wanted.
func TestAFailedCallForAStepWithdrawnMeanwhileIsDroppedNotRaised(t *testing.T) {
	h := newEngineHarness(t, "AdHoc Withdrawn Failure Project")
	ctx := h.Ctx()

	var instanceID uuid.UUID
	var withdrawErr error
	api, calls := partnerAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		// While the insurer is being called, somebody finishes the step that
		// ends the sub-process.
		withdrawErr = completeOpenTaskAt(ctx, h, instanceID, "call-customer", map[string]any{"reviewsDone": 1})
		http.Error(w, "the insurer's system is down", http.StatusInternalServerError)
	})

	def := adHocDefinition("claim-research-failing-call", "reviewsDone >= 1")
	def.Project = &entities.Project{ID: h.projID}
	withANotification(&def, api.URL)
	h.deploy(t, &def)
	var err error
	instanceID, err = h.svc.StartProcess(ctx, h.projID, "claim-research-failing-call", map[string]any{"reviewsDone": 0})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	for _, step := range []string{"call-customer", "notify-insurer"} {
		if err := h.svc.ActivateTask(ctx, instanceID, "research", step); err != nil {
			t.Fatalf("activate %s: %v", step, err)
		}
	}
	// One attempt is all it gets, so a failure that is counted is raised.
	queued := jobsOn(ctx, t, h, instanceID, "notify-insurer")
	if len(queued) != 1 {
		t.Fatalf("the notification was queued %d times, want once", len(queued))
	}
	queued[0].MaxRetries = 1
	if err := h.repo.Job().Update(ctx, queued[0]); err != nil {
		t.Fatalf("limit the job to one attempt: %v", err)
	}

	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if withdrawErr != nil {
		t.Fatalf("finishing the sub-process during the call: %v", withdrawErr)
	}
	if made := calls.Load(); made == 0 {
		t.Fatal("the call was never made, so nothing failed and nothing was shown")
	}
	if !h.waitingAt(ctx, t, instanceID, "decide") {
		t.Fatal("the completion condition was met and the process did not carry on")
	}
	incidents, err := h.jobSvc.ListIncidents(ctx, instanceID)
	if err != nil {
		t.Fatalf("list incidents: %v", err)
	}
	if len(incidents) != 0 {
		t.Fatalf("a failed call for a withdrawn step was raised as %d incident(s): %+v", len(incidents), incidents)
	}
	job := jobsOn(ctx, t, h, instanceID, "notify-insurer")[0]
	if job.Status != models.JobCompleted || job.Retries != 0 {
		t.Fatalf("the withdrawn step's job is %s with %d failure(s) counted, want completed and none", job.Status, job.Retries)
	}
}

// completeOpenTaskAt finishes the open task at nodeID and returns what went
// wrong rather than failing the test: it is called from a stub endpoint's
// goroutine, where a test may not be failed.
func completeOpenTaskAt(ctx context.Context, h engineHarness, instanceID uuid.UUID, nodeID string, vars map[string]any) error {
	tasks, err := h.svc.ListTasks(ctx, h.projID)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == nodeID && taskIsOpen(task.Status) {
			return h.svc.CompleteTask(testutils.AsOperator(ctx, "carol"), task.ID, "carol", vars)
		}
	}
	return nil
}
