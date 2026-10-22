package bpmn_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A service task runs when the step that queued it commits, not at the job
// worker's next poll.
//
// Each one waited for the tick: two seconds at the default interval, so three
// service tasks in a row took six seconds to reach the person after them, all
// of it idle. The interval here is an hour, so a test that passes is one where
// nothing waited for it.
func TestServiceTasksInARowRunWithoutWaitingForThePoll(t *testing.T) {
	// Read when the job service is built, so it is set before the harness.
	t.Setenv("METIS_JOB_POLL_INTERVAL", "1h")

	var calls atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	h := newServiceTaskHarness(t)
	call := func(id string) *entities.Node {
		return &entities.Node{ID: id, Name: "Call " + id, Type: entities.ServiceTask,
			Properties: map[string]any{"http_url": api.URL, "http_method": "POST"}}
	}
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projectID},
		Key:     "three-calls-then-review",
		Name:    "Three calls, then a review",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			call("one"), call("two"), call("three"),
			{ID: "review", Type: entities.UserTask, Name: "Review"},
			{ID: "end", Type: entities.EndEvent, Name: "End"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "one"},
			{ID: "f2", SourceRef: "one", TargetRef: "two"},
			{ID: "f3", SourceRef: "two", TargetRef: "three"},
			{ID: "f4", SourceRef: "three", TargetRef: "review"},
			{ID: "f5", SourceRef: "review", TargetRef: "end"},
		},
	}
	if _, err := h.defSvc.CreateDefinition(h.ctx, def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	workerCtx, stopClaiming := context.WithCancel(t.Context())
	h.jobSvc.StartWorkers(workerCtx)
	t.Cleanup(func() {
		stopClaiming()
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
		defer cancel()
		if err := h.jobSvc.StopWorkers(stopCtx); err != nil {
			t.Errorf("stop the job worker: %v", err)
		}
	})

	instanceID, err := h.engine.StartProcess(h.ctx, h.projectID, def.Key, nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	// Far below the hour a tick would take, and far above the milliseconds
	// three local calls do.
	deadline := time.Now().Add(15 * time.Second)
	for {
		instance, err := h.engine.GetInstance(h.ctx, instanceID)
		if err != nil {
			t.Fatalf("reload instance: %v", err)
		}
		if waitsAt(instance, "review") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after %d of 3 calls the process had not reached the review: the worker waited for its poll", calls.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("the endpoint was called %d times, want 3", got)
	}
}

func waitsAt(instance entities.ProcessInstance, nodeID string) bool {
	for _, token := range instance.Tokens {
		if token.Node != nil && token.Node.ID == nodeID && token.Status == entities.TokenActive {
			return true
		}
	}
	return false
}
