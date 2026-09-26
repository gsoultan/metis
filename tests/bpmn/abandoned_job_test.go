package bpmn_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// Work whose worker died.
//
// The queue offered pending jobs only, so a job left running — its pod killed,
// the process crashed, its final status write failed — was never offered to
// anybody again, and the process waiting behind it hung with no incident. The
// fix reclaims a running job once its lease has expired; these tests stand in
// for the dead worker by claiming a job with a lease that is already over.

// waitThenReview is a process that waits on a timer and then gives somebody a
// task, so which side of the timer it is on is visible.
func waitThenReview(t *testing.T, h engineHarness, key string) uuid.UUID {
	t.Helper()
	ctx := h.Ctx()
	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "wait", Type: entities.IntermediateCatchEvent, Properties: map[string]any{"timer_duration": "PT2H"}},
			{ID: "review", Type: entities.UserTask, Name: "Review"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "wait"},
			{ID: "f2", SourceRef: "wait", TargetRef: "review"},
			{ID: "f3", SourceRef: "review", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	instanceID, err := h.svc.StartProcess(ctx, h.projID, key, nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	if moved := h.dueNow(ctx, t, instanceID); moved != 1 {
		t.Fatalf("expected the one timer job to bring forward, found %d", moved)
	}
	return instanceID
}

// onlyJob returns the instance's single job, as stored.
func (h engineHarness) onlyJob(ctx context.Context, t *testing.T, instanceID uuid.UUID) models.JobModel {
	t.Helper()
	jobs, err := h.repo.Job().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected one job, found %d", len(jobs))
	}
	return jobs[0]
}

// abandon leaves a job the way a worker that died leaves it: running, under a
// lease that has run out.
func (h engineHarness) abandon(ctx context.Context, t *testing.T, job models.JobModel) {
	t.Helper()
	expired := time.Now().Add(-time.Minute)
	job.Status = models.JobRunning
	job.LockedBy = "a worker that died"
	job.LockExpires = &expired
	if err := h.repo.Job().Update(ctx, job); err != nil {
		t.Fatalf("abandon the job: %v", err)
	}
}

func (h engineHarness) reviewTasks(ctx context.Context, t *testing.T, instanceID uuid.UUID) int {
	t.Helper()
	tasks, err := h.svc.ListTasks(ctx, h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	count := 0
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == "review" {
			count++
		}
	}
	return count
}

func TestAJobWhoseWorkerDiedIsPickedUpAgain(t *testing.T) {
	h := newEngineHarness(t, "Abandoned Job Project")
	ctx := h.Ctx()
	instanceID := waitThenReview(t, h, "abandoned-timer")

	h.abandon(ctx, t, h.onlyJob(ctx, t, instanceID))
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if !h.waitingAt(ctx, t, instanceID, "review") {
		t.Fatal("the timer whose worker died never fired; the process is stuck behind it")
	}
	job := h.onlyJob(ctx, t, instanceID)
	if job.Status != models.JobCompleted {
		t.Fatalf("the reclaimed job is %s, want completed", job.Status)
	}
	if job.Retries != 1 {
		t.Errorf("the attempt the dead worker lost was not counted: retries = %d", job.Retries)
	}
}

// Reclaiming is only safe if work that already committed is not done again.
// This is the state a worker leaves when it dies after the work and before
// the job says so — which the engine could not survive: advancing a node the
// token has already left follows its outgoing flows a second time.
func TestAReclaimedJobWhoseWorkCommittedDoesNotAdvanceTheProcessTwice(t *testing.T) {
	h := newEngineHarness(t, "Committed Work Project")
	ctx := h.Ctx()
	instanceID := waitThenReview(t, h, "committed-timer")

	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}
	if h.reviewTasks(ctx, t, instanceID) != 1 {
		t.Fatal("the timer did not advance the process the first time")
	}

	// The worker died before the job's status reached the database.
	h.abandon(ctx, t, h.onlyJob(ctx, t, instanceID))
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs again: %v", err)
	}

	if got := h.reviewTasks(ctx, t, instanceID); got != 1 {
		t.Fatalf("the process advanced past the timer again: %d review tasks", got)
	}
	if job := h.onlyJob(ctx, t, instanceID); job.Status != models.JobCompleted {
		t.Fatalf("the reclaimed job is %s, want completed", job.Status)
	}
}

// A job that kills its worker every time — a script that runs a pod out of
// memory — must end as something somebody can see, not go round the fleet.
func TestAJobThatKeepsKillingItsWorkerEndsAsAnIncident(t *testing.T) {
	h := newEngineHarness(t, "Poison Job Project")
	ctx := h.Ctx()
	instanceID := waitThenReview(t, h, "poison-timer")

	job := h.onlyJob(ctx, t, instanceID)
	job.Retries = 2 // two workers already lost to it
	h.abandon(ctx, t, job)
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	if h.waitingAt(ctx, t, instanceID, "review") {
		t.Fatal("the job was run a fourth time instead of being stopped")
	}
	if got := h.onlyJob(ctx, t, instanceID); got.Status != models.JobFailed {
		t.Fatalf("the job is %s, want failed", got.Status)
	}
	incidents, err := h.jobSvc.ListIncidents(ctx, instanceID)
	if err != nil {
		t.Fatalf("list incidents: %v", err)
	}
	if len(incidents) != 1 || !strings.Contains(incidents[0].Error, "stopped before it finished") {
		t.Fatalf("expected one incident saying the worker kept stopping, got %+v", incidents)
	}
}

// A repeating reminder on an activity that has finished has nothing left to
// remind anybody of. It used to queue its next occurrence anyway, and for an
// unbounded cycle, forever.
func TestARepeatingBoundaryTimerStopsOnceItsActivityHasFinished(t *testing.T) {
	h := newEngineHarness(t, "Finished Reminder Project")
	ctx := h.Ctx()

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "reminded-approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "approve", Type: entities.UserTask, Name: "Approve"},
			{ID: "remind", Type: entities.BoundaryEvent, AttachedToRef: "approve",
				Properties: map[string]any{"timer_duration": "R/PT10M", "non_interrupting": true}},
			{ID: "nudge", Type: entities.UserTask, Name: "Nudge the approver"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "end"},
			{ID: "f3", SourceRef: "remind", TargetRef: "nudge"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "reminded-approval", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	tasks, err := h.svc.ListTasks(ctx, h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == "approve" {
			if err := h.svc.CompleteTask(testutils.AsOperator(ctx, "carol"), task.ID, "carol", nil); err != nil {
				t.Fatalf("approve: %v", err)
			}
		}
	}

	if moved := h.dueNow(ctx, t, instanceID); moved == 0 {
		t.Fatal("no reminder was waiting to come due")
	}
	if err := h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}

	jobs, err := h.repo.Job().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	for _, job := range jobs {
		if job.Status == models.JobPending {
			t.Fatalf("the reminder queued another occurrence for an approval that is done (next at %s)", job.NextRunAt)
		}
	}
}

// An external task's lock is a lease for the same reason: a worker that dies
// holding one must not strand the work. The query that offers tasks asked for
// "no lease, or an expired one" in an order the generated builder mishandles —
// it dropped the second half — so only never-locked tasks were ever offered.
func TestAnExternalTaskWhoseWorkerDiedIsOfferedAgain(t *testing.T) {
	h := newEngineHarness(t, "Abandoned External Task Project")
	ctx := h.Ctx()

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "leased-work",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: "work", Type: entities.ServiceTask, ExternalTopic: "lease-topic", Incoming: []string{"f1"}, Outgoing: []string{"f2"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "work"},
			{ID: "f2", SourceRef: "work", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	if _, err := h.svc.StartProcess(ctx, h.projID, "leased-work", nil); err != nil {
		t.Fatalf("start process: %v", err)
	}

	held, err := h.svc.FetchAndLock(ctx, "lease-topic", "worker-that-dies", 1, 60_000)
	if err != nil || len(held) != 1 {
		t.Fatalf("the first worker should get the task: tasks=%d err=%v", len(held), err)
	}
	if others, err := h.svc.FetchAndLock(ctx, "lease-topic", "second-worker", 1, 60_000); err != nil || len(others) != 0 {
		t.Fatalf("a held lease must not be offered to another worker: tasks=%d err=%v", len(others), err)
	}

	// The first worker dies, and its lease runs out.
	task, err := h.repo.ExternalTask().Get(ctx, held[0].ID)
	if err != nil {
		t.Fatalf("read the task: %v", err)
	}
	expired := time.Now().Add(-time.Minute)
	task.LockExpiration = &expired
	if err := h.repo.ExternalTask().Update(ctx, task); err != nil {
		t.Fatalf("expire the lease: %v", err)
	}

	offered, err := h.svc.FetchAndLock(ctx, "lease-topic", "second-worker", 1, 60_000)
	if err != nil {
		t.Fatalf("fetch after the lease expired: %v", err)
	}
	if len(offered) != 1 {
		t.Fatal("a task whose worker died is never offered to anybody again; the process is stuck behind it")
	}
}

// The same for a service task, which had no guard of its own: a timer checks
// its token is still there before it fires, and a service task advanced
// whether or not the token was there to advance. The call itself was never at
// risk — it is recorded and its response reused — but the advance ran again,
// and the process went down its outgoing flows twice.
func TestAReclaimedServiceTaskWhoseAdvanceCommittedDoesNotAdvanceAgain(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"charge_id":"ch_1"}`))
	}))
	defer api.Close()

	h := newServiceTaskHarness(t)
	instance := h.runDefinition(t, &entities.ProcessDefinition{
		Key:  "charge-then-review",
		Name: "Charge, then review",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "charge", Type: entities.ServiceTask, Properties: map[string]any{
				"http_url": api.URL, "http_method": "POST", "output_charge_id": "chargeId",
			}},
			{ID: "review", Type: entities.UserTask, Name: "Review the charge"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "charge"},
			{ID: "f2", SourceRef: "charge", TargetRef: "review"},
			{ID: "f3", SourceRef: "review", TargetRef: "end"},
		},
	}, nil)

	reviewing := func() int {
		reloaded, err := h.engine.GetInstance(h.ctx, instance.ID)
		if err != nil {
			t.Fatalf("reload instance: %v", err)
		}
		return len(reloaded.GetTokensByNode(&entities.Node{ID: "review"}))
	}
	if got := reviewing(); got != 1 {
		t.Fatalf("after the charge the process should wait on one review, found %d", got)
	}

	// The worker committed the advance and died before the job said so.
	jobs, err := h.repo.Job().ListByInstance(h.ctx, instance.ID)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("expected the charge's one job: jobs=%d err=%v", len(jobs), err)
	}
	expired := time.Now().Add(-time.Minute)
	job := jobs[0]
	job.Status = models.JobRunning
	job.LockedBy = "a worker that died"
	job.LockExpires = &expired
	if err := h.repo.Job().Update(h.ctx, job); err != nil {
		t.Fatalf("abandon the job: %v", err)
	}

	if err := h.jobSvc.ProcessPendingJobs(h.ctx); err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if got := reviewing(); got != 1 {
		t.Fatalf("reclaiming the charge advanced the process again: %d reviews waiting", got)
	}
}
