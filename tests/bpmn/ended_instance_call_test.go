package bpmn_test

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// callingAPartner is the step under test as a call the engine makes itself.
func callingAPartner(url string) *entities.Node {
	return &entities.Node{Type: entities.ServiceTask, Name: "Notify the carrier",
		Properties: map[string]any{"http_url": url, "http_method": "POST"}}
}

// runDueJobs brings every pending job of the instance due and runs one round
// of what is due.
func (r *fulfilmentRun) runDueJobs(t *testing.T) {
	t.Helper()
	ctx := r.h.Ctx()
	jobs, err := r.h.repo.Job().ListByInstance(ctx, r.id)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	for _, job := range jobs {
		if job.Status != models.JobPending {
			continue
		}
		job.NextRunAt = time.Now().Add(-time.Minute)
		if err := r.h.repo.Job().Update(ctx, job); err != nil {
			t.Fatalf("bring a job due: %v", err)
		}
	}
	if err := r.h.jobSvc.ProcessPendingJobs(ctx); err != nil {
		t.Fatalf("run the due jobs: %v", err)
	}
}

// theCall is the one job queued for the step under test.
func (r *fulfilmentRun) theCall(t *testing.T) models.JobModel {
	t.Helper()
	jobs, err := r.h.repo.Job().ListByInstance(r.h.Ctx(), r.id)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	var calls []models.JobModel
	for _, job := range jobs {
		if job.NodeID == "work" {
			calls = append(calls, job)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("%d job(s) are queued for the step, want exactly one", len(calls))
	}
	return calls[0]
}

// No call is made to a partner on behalf of an instance that has ended. A call
// is the part that cannot be taken back: a carrier booked, a card charged, a
// customer written to, for an order somebody called off.
//
// The call is queued when the instance reaches the step, and the queue is not
// told when the instance ends. So the job asks before it calls.
func TestNoCallIsMadeForAnInstanceThatHasEnded(t *testing.T) {
	for _, ending := range endings {
		t.Run(ending.name+", before the call was tried", func(t *testing.T) {
			api, calls := countingCalls(t, respondOK)
			h := newEngineHarness(t, "Ended Instance Call Project")
			r := startFulfilment(t, h, "fulfilment-call", func() *entities.Node { return callingAPartner(api) })
			if queued := r.theCall(t); queued.Status != models.JobPending {
				t.Fatalf("the call is %s before anything ran; this test needs it pending", queued.Status)
			}
			if err := ending.end(t, r); err != nil {
				t.Fatalf("end the instance: %v", err)
			}

			r.runDueJobs(t)
			if made := calls.Load(); made != 0 {
				t.Errorf("%d call(s) were made for an instance that had ended", made)
			}
			if settled := r.theCall(t); settled.Status != models.JobCompleted {
				t.Errorf("the call's job is %s, want it settled so that nothing runs it again", settled.Status)
			}
			if open := r.openIncidents(t); open != 0 {
				t.Errorf("%d incident(s) were raised on an instance that had ended", open)
			}
			r.requireEndedAndStill(t, ending.status)
		})

		t.Run(ending.name+", while the call waits to be tried again", func(t *testing.T) {
			api, calls := countingCalls(t, func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "the carrier's system is down", http.StatusInternalServerError)
			})
			h := newEngineHarness(t, "Ended Instance Retry Project")
			r := startFulfilment(t, h, "fulfilment-retry", func() *entities.Node { return callingAPartner(api) })
			r.runDueJobs(t)
			if waiting := r.theCall(t); calls.Load() != 1 || waiting.Status != models.JobPending || waiting.Retries != 1 {
				t.Fatalf("after one failed call: %d call(s), the job %s with %d tries used; this test needs one, pending, one",
					calls.Load(), waiting.Status, waiting.Retries)
			}
			if err := ending.end(t, r); err != nil {
				t.Fatalf("end the instance: %v", err)
			}

			// As many rounds as would have used up the job's tries.
			for range 3 {
				r.runDueJobs(t)
			}
			if made := calls.Load(); made != 1 {
				t.Errorf("%d call(s) were made in all; the %d after the instance ended were made for nobody", made, made-1)
			}
			if settled := r.theCall(t); settled.Status != models.JobCompleted {
				t.Errorf("the call's job is %s, want it settled so that nothing runs it again", settled.Status)
			}
			if open := r.openIncidents(t); open != 0 {
				t.Errorf("%d incident(s) were raised on an instance that had ended", open)
			}
			r.requireEndedAndStill(t, ending.status)
		})
	}
}

// An instance can end while a call for it is on its way: the call is made
// outside any transaction, so nothing holds the instance while the partner
// answers, and it cannot be taken back. When that call then fails for the
// last time, the failure is recorded on the job — and no incident is raised
// for somebody to resolve on an instance nobody is running.
func TestACallThatFailsAsItsInstanceEndsRaisesNoIncident(t *testing.T) {
	// The third call is the job's last try. The partner has it, and does not
	// answer until the instance has been cancelled.
	var answered atomic.Int32
	arrived, cancelled := make(chan struct{}), make(chan error, 1)
	api, calls := countingCalls(t, func(w http.ResponseWriter, req *http.Request) {
		if answered.Add(1) == 3 {
			close(arrived)
			select {
			case <-cancelled:
			case <-req.Context().Done():
			}
		}
		http.Error(w, "the carrier's system is down", http.StatusInternalServerError)
	})
	h := newEngineHarness(t, "Ended Mid Call Project")
	r := startFulfilment(t, h, "fulfilment-mid-call", func() *entities.Node { return callingAPartner(api) })
	var endErr error
	go func() {
		<-arrived
		endErr = r.cancelByMigration()
		close(cancelled)
	}()

	for range 3 {
		r.runDueJobs(t)
	}
	select {
	case <-cancelled:
	default:
		t.Fatalf("the instance was not cancelled during the third call; %d call(s) were made", calls.Load())
	}
	if endErr != nil {
		t.Fatalf("end the instance during the call: %v", endErr)
	}
	if made := calls.Load(); made != 3 {
		t.Fatalf("%d call(s) were made; this test needs the three the job has tries for", made)
	}
	r.requireEndedAndStill(t, entities.ProcessCancelled)
	if failed := r.theCall(t); failed.Status != models.JobFailed || failed.LastError == "" {
		t.Errorf("the call's job is %s with the error %q, want it failed and saying why", failed.Status, failed.LastError)
	}
	if open := r.openIncidents(t); open != 0 {
		t.Errorf("%d incident(s) were raised on an instance that had ended", open)
	}
}

// A call that fails for the last time on an instance that is still running
// raises its incident as it always did.
func TestACallThatFailsOnARunningInstanceRaisesItsIncident(t *testing.T) {
	api, calls := countingCalls(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "the carrier's system is down", http.StatusInternalServerError)
	})
	h := newEngineHarness(t, "Running Instance Call Project")
	r := startFulfilment(t, h, "fulfilment-failing", func() *entities.Node { return callingAPartner(api) })
	for range 3 {
		r.runDueJobs(t)
	}
	if made := calls.Load(); made != 3 {
		t.Fatalf("%d call(s) were made, want the three the job has tries for", made)
	}
	if failed := r.theCall(t); failed.Status != models.JobFailed {
		t.Errorf("the call's job is %s, want it failed", failed.Status)
	}
	if open := r.openIncidents(t); open != 1 {
		t.Errorf("%d incident(s) are open on a running instance whose call failed for good, want one", open)
	}
}
