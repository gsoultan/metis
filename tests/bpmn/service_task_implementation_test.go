package bpmn_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// How a service task is carried out is the modeller's choice, and the designer
// records it as "implementation" while keeping what was typed under every
// choice. The engine never read the choice: a topic on the node meant a
// worker, whatever had been chosen since, and an address or a topic under the
// names definitions saved from August to September 2026 hold meant nothing at
// all — the step was taken for a simulated one and the process moved on as
// though it had been done.

// counting answers every call and counts them.
func counting(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(api.Close)
	return api, &calls
}

func TestAStepSwitchedToAWebAddressCallsItRatherThanWaitingForAWorker(t *testing.T) {
	api, calls := counting(t)
	h := newServiceTaskHarness(t)

	instance := h.run(t, entities.Node{
		ID:            "notify",
		Name:          "Tell the carrier",
		Type:          entities.ServiceTask,
		ExternalTopic: "carrier-worker", // typed before the modeller switched
		Properties:    map[string]any{"implementation": "push", "http_url": api.URL},
	}, nil)

	if calls.Load() != 1 {
		t.Fatalf("the web address was called %d times; the step waited for a worker on the topic it no longer uses", calls.Load())
	}
	if instance.Status != entities.ProcessCompleted {
		t.Fatalf("the process is %s after the step called its web address", instance.Status)
	}
}

func TestAWebAddressSavedUnderItsOlderNameIsCalled(t *testing.T) {
	api, calls := counting(t)
	h := newServiceTaskHarness(t)

	h.run(t, entities.Node{
		ID:         "notify",
		Name:       "Tell the carrier",
		Type:       entities.ServiceTask,
		Properties: map[string]any{"implementation": "push", "url": api.URL},
	}, nil)

	if calls.Load() != 1 {
		t.Fatalf("the web address the designer shows was called %d times; the step was taken for a simulated one", calls.Load())
	}
}

func TestATopicSavedUnderItsOlderNameWaitsForAWorker(t *testing.T) {
	h := newServiceTaskHarness(t)

	instance := h.run(t, entities.Node{
		ID:         "ship",
		Name:       "Ship the parcel",
		Type:       entities.ServiceTask,
		Properties: map[string]any{"implementation": "external", "topic": "ship-parcel"},
	}, nil)

	if instance.Status != entities.ProcessActive {
		t.Fatalf("the process is %s; it should be waiting for a worker on ship-parcel", instance.Status)
	}
	parked, err := h.repo.ExternalTask().ListByProcessInstance(h.ctx, instance.ID)
	if err != nil {
		t.Fatalf("list the external tasks: %v", err)
	}
	if len(parked) != 1 || parked[0].Topic != "ship-parcel" {
		t.Fatalf("expected one task on ship-parcel for a worker, found %d", len(parked))
	}
}

func TestAWorkerStepWithNoTopicFailsWhereSomeoneSeesIt(t *testing.T) {
	h := newServiceTaskHarness(t)
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projectID},
		Key:     "worker-without-topic",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "ship", Name: "Ship the parcel", Type: entities.ServiceTask,
				Properties: map[string]any{"implementation": "external"}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "ship"},
			{ID: "f2", SourceRef: "ship", TargetRef: "end"},
		},
	}
	if _, err := h.defSvc.CreateDefinition(h.ctx, def); err != nil {
		t.Fatalf("deploy: %v", err)
	}

	_, err := h.engine.StartProcess(h.ctx, h.projectID, def.Key, nil)
	if err == nil {
		t.Fatal("a step waiting for a worker under no topic started, and nothing could ever ask for it")
	}
	if !strings.Contains(err.Error(), "ship") || !strings.Contains(err.Error(), "topic") {
		t.Fatalf("the failure does not say which step lacks a topic: %v", err)
	}
}
