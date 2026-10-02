package bpmn_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// chargeProcess is start → a step a worker performs → end.
func chargeProcess(projectID uuid.UUID) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "charge-card",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "charge", Type: entities.ServiceTask, Name: "Charge the card", ExternalTopic: "charge"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "charge"},
			{ID: "f2", SourceRef: "charge", TargetRef: "end"},
		},
	}
}

// A worker that gives up on an external task — no retries left — was logged
// and nothing else: no incident, so nobody was told, and the task went back on
// offer to be fetched and failed again. The instance waited at the step with
// nothing to investigate. It raises an incident now, stops being offered, and
// resolving the incident offers it again.
func TestAnExternalTaskThatRunsOutOfRetriesRaisesAnIncident(t *testing.T) {
	h := newEngineHarness(t, "External Failure Project")
	ctx := h.Ctx()
	h.deploy(t, chargeProcess(h.projID))
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "charge-card", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	fetched, err := h.svc.FetchAndLock(ctx, "charge", "worker-1", 1, 60_000)
	if err != nil || len(fetched) != 1 {
		t.Fatalf("fetch: %d tasks, %v", len(fetched), err)
	}
	if err := h.svc.HandleFailure(ctx, fetched[0].ID, "worker-1", "card declined", "issuer said no", 0, 0); err != nil {
		t.Fatalf("report the failure: %v", err)
	}

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
	if len(open) != 1 || open[0].NodeID != "charge" {
		t.Fatalf("a worker gave up and the instance has %d open incident(s): %+v", len(open), open)
	}
	if again, err := h.svc.FetchAndLock(ctx, "charge", "worker-2", 1, 60_000); err != nil || len(again) != 0 {
		t.Fatalf("a task with no retries left was offered again: %d, %v", len(again), err)
	}

	if err := h.svc.ResolveIncident(ctx, uuid.UUID(open[0].ID)); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if again, err := h.svc.FetchAndLock(ctx, "charge", "worker-2", 1, 60_000); err != nil || len(again) != 1 {
		t.Fatalf("resolving the incident did not offer the task again: %d, %v", len(again), err)
	}
}

// A worker that fails with retries left says how long to wait before the next
// try. The wait was stored and never read: the task was offered again at once,
// to fail again against whatever had just failed it.
func TestAnExternalTaskWaitsOutItsRetryTimeout(t *testing.T) {
	h := newEngineHarness(t, "External Retry Project")
	ctx := h.Ctx()
	h.deploy(t, chargeProcess(h.projID))
	if _, err := h.svc.StartProcess(ctx, h.projID, "charge-card", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	fetched, err := h.svc.FetchAndLock(ctx, "charge", "worker-1", 1, 60_000)
	if err != nil || len(fetched) != 1 {
		t.Fatalf("fetch: %d tasks, %v", len(fetched), err)
	}
	if err := h.svc.HandleFailure(ctx, fetched[0].ID, "worker-1", "gateway timeout", "", 2, 60_000); err != nil {
		t.Fatalf("report the failure: %v", err)
	}
	if again, err := h.svc.FetchAndLock(ctx, "charge", "worker-2", 1, 60_000); err != nil || len(again) != 0 {
		t.Fatalf("a task told to wait a minute was offered again at once: %d, %v", len(again), err)
	}
}

// A report about a task from a worker that does not hold its lock was a bare
// error with the task's id in it, which the transport answers as a failure of
// the server's: a worker's client retried a report that could never be taken.
// It is a refusal, as extending a lock one does not hold is — whether the
// report is that the work is done or that it failed.
func TestAReportFromAWorkerWithoutTheLockIsRefusedNotFailed(t *testing.T) {
	h := newEngineHarness(t, "External Refusal Project")
	ctx := h.Ctx()
	h.deploy(t, chargeProcess(h.projID))
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "charge-card", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	fetched, err := h.svc.FetchAndLock(ctx, "charge", "worker-1", 1, 60_000)
	if err != nil || len(fetched) != 1 {
		t.Fatalf("fetch: %d tasks, %v", len(fetched), err)
	}
	taskID := fetched[0].ID

	for name, report := range map[string]func() error{
		"done":   func() error { return h.svc.Complete(ctx, taskID, "worker-2", nil) },
		"failed": func() error { return h.svc.HandleFailure(ctx, taskID, "worker-2", "card declined", "", 0, 0) },
	} {
		err := report()
		if !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Fatalf("%s, from a worker without the lock: got %v, want a refusal", name, err)
		}
		if strings.Contains(err.Error(), taskID.String()) {
			t.Fatalf("the refusal names the task's id: %q", err)
		}
	}

	// Nothing was taken from the worker that does hold it.
	incidents, err := h.repo.Incident().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("read the incidents: %v", err)
	}
	if len(incidents) != 0 {
		t.Fatalf("a refused failure raised %d incident(s)", len(incidents))
	}
	if err := h.svc.Complete(ctx, taskID, "worker-1", nil); err != nil {
		t.Fatalf("the worker holding the lock could not complete: %v", err)
	}
}
