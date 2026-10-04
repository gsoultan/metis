package bpmn_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// failingCarrier is a partner that always fails.
func failingCarrier(t *testing.T) string {
	t.Helper()
	api, _ := countingCalls(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "the carrier's system is down", http.StatusInternalServerError)
	})
	return api
}

// theOpenIncident is the one incident an instance has open.
func (r *fulfilmentRun) theOpenIncident(t *testing.T) uuid.UUID {
	t.Helper()
	incidents, err := r.h.svc.ListIncidents(r.h.Ctx(), r.id)
	if err != nil {
		t.Fatalf("read the incidents: %v", err)
	}
	var open []entities.Incident
	for _, incident := range incidents {
		if incident.Status == entities.IncidentOpen {
			open = append(open, incident)
		}
	}
	if len(open) != 1 {
		t.Fatalf("the instance has %d open incident(s), want exactly one", len(open))
	}
	return open[0].ID
}

// requireClosed fails unless an incident is resolved, says when, and still
// says what went wrong: what it says is evidence, and is not rewritten.
func (r *fulfilmentRun) requireClosed(t *testing.T, id uuid.UUID, saying string) {
	t.Helper()
	incidents, err := r.h.svc.ListIncidents(r.h.Ctx(), r.id)
	if err != nil {
		t.Fatalf("read the incidents: %v", err)
	}
	for _, incident := range incidents {
		if incident.ID != id {
			continue
		}
		if incident.Status != entities.IncidentResolved || incident.ResolvedAt == nil {
			t.Fatalf("the incident is %s (resolved at %v) on a cancelled instance, want it closed", incident.Status, incident.ResolvedAt)
		}
		if !strings.Contains(incident.Error, saying) {
			t.Errorf("the closed incident reads %q; it no longer says %q", incident.Error, saying)
		}
		return
	}
	t.Fatalf("the incident is gone; closing one does not remove it")
}

// requireNoAttention fails unless the instance list counts nothing of this
// project as needing somebody's attention, and marks this instance with no
// open incident.
func (r *fulfilmentRun) requireNoAttention(t *testing.T) {
	t.Helper()
	attention, err := r.h.engine.InstanceAttention(r.h.Ctx(), r.h.projID, repocontracts.InstanceFilter{}, []uuid.UUID{r.id})
	if err != nil {
		t.Fatalf("read what needs attention: %v", err)
	}
	if attention.Total != 0 || attention.OpenIncidents[r.id] != 0 {
		t.Errorf("the instance list says %d instance(s) need attention and marks this one with %d open incident(s); it was cancelled",
			attention.Total, attention.OpenIncidents[r.id])
	}
}

// An incident asks somebody to look at an instance and decide. Once the
// instance is cancelled there is nothing left to decide, and an incident left
// open on it sits in the inbox for good: resolving it would retry a job, or
// offer work again, for an instance nobody is running. So a cancel closes the
// incidents the instance has open — directly, without doing what resolving
// one does.
func TestACancelClosesTheIncidentsOpenOnTheInstance(t *testing.T) {
	t.Run("a call that failed for good", func(t *testing.T) {
		api := failingCarrier(t)
		h := newEngineHarness(t, "Cancel Closes Job Incident Project")
		r := startFulfilment(t, h, "fulfilment-failed-call", func() *entities.Node { return callingAPartner(api) })
		for range 3 {
			r.runDueJobs(t)
		}
		incident := r.theOpenIncident(t)
		if err := r.cancelByMigration(); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		r.requireEndedAndStill(t, entities.ProcessCancelled)

		r.requireClosed(t, incident, "500")
		r.requireNoAttention(t)
		// Closed, not resolved: the job behind it is not put back to be tried.
		if job := r.theCall(t); job.Status != models.JobFailed {
			t.Errorf("the failed call's job is %s after the cancel, want it left failed", job.Status)
		}
	})

	t.Run("a worker that gave up", func(t *testing.T) {
		h := newEngineHarness(t, "Cancel Closes Worker Incident Project")
		ctx := h.Ctx()
		r := startFulfilment(t, h, "fulfilment-gave-up", forAWorker)
		fetched, err := h.svc.FetchAndLock(ctx, "ship", "worker-1", 1, 60_000)
		if err != nil || len(fetched) != 1 {
			t.Fatalf("fetch: %d task(s), %v", len(fetched), err)
		}
		if err := h.svc.HandleFailure(ctx, fetched[0].ID, "worker-1", "the carrier's system is down", "", 0, 0); err != nil {
			t.Fatalf("give up: %v", err)
		}
		incident := r.theOpenIncident(t)
		if err := r.cancelByMigration(); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		r.requireEndedAndStill(t, entities.ProcessCancelled)

		r.requireClosed(t, incident, "the carrier's system is down")
		r.requireNoAttention(t)
		if left := r.parked(t); len(left) != 0 {
			t.Errorf("%d piece(s) of work are still parked for a cancelled instance", len(left))
		}
	})

	t.Run("an incident already resolved is left as it was", func(t *testing.T) {
		h := newEngineHarness(t, "Cancel Leaves Resolved Incident Project")
		ctx := h.Ctx()
		r := startFulfilment(t, h, "fulfilment-resolved", forAWorker)
		fetched, err := h.svc.FetchAndLock(ctx, "ship", "worker-1", 1, 60_000)
		if err != nil || len(fetched) != 1 {
			t.Fatalf("fetch: %d task(s), %v", len(fetched), err)
		}
		if err := h.svc.HandleFailure(ctx, fetched[0].ID, "worker-1", "the carrier's system is down", "", 0, 0); err != nil {
			t.Fatalf("give up: %v", err)
		}
		incident := r.theOpenIncident(t)
		if err := h.svc.ResolveIncident(ctx, incident); err != nil {
			t.Fatalf("resolve: %v", err)
		}
		before, err := h.svc.ListIncidents(ctx, r.id)
		if err != nil || len(before) != 1 || before[0].ResolvedAt == nil {
			t.Fatalf("the resolved incident: %+v, %v", before, err)
		}
		if err := r.cancelByMigration(); err != nil {
			t.Fatalf("cancel: %v", err)
		}
		after, err := h.svc.ListIncidents(ctx, r.id)
		if err != nil || len(after) != 1 || after[0].ResolvedAt == nil || !after[0].ResolvedAt.Equal(*before[0].ResolvedAt) {
			t.Fatalf("the cancel rewrote an incident that was already resolved: %+v, %v", after, err)
		}
	})
}
