package bpmn_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A step that repeats once per item reads its list from a variable. A list
// that is missing, is not a list, or is too long to run is refused, naming the
// step and the variable; it used to run the step once with no item, or create
// a token per item until memory ran out.

// collectionWorld is a service task harness with an endpoint that counts the
// calls it gets.
type collectionWorld struct {
	h     *serviceTaskHarness
	calls *atomic.Int64
	url   string
}

func newCollectionWorld(t *testing.T) collectionWorld {
	t.Helper()
	calls := &atomic.Int64{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(api.Close)
	return collectionWorld{h: newServiceTaskHarness(t), calls: calls, url: api.URL}
}

// notifyEach is a process whose one step calls the endpoint once for each item
// in the collection named.
func (w collectionWorld) notifyEach(t *testing.T, key, collection string) {
	t.Helper()
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: w.h.projectID},
		Key:     key,
		Name:    "Notify each supplier",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{
				ID: "notify", Name: "Notify each supplier", Type: entities.ServiceTask,
				MultiInstanceType: "parallel", Collection: collection, ElementVariable: "supplier",
				Properties: map[string]any{"http_url": w.url, "http_method": "POST"},
			},
			{ID: "end", Type: entities.EndEvent, Name: "End"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "notify"},
			{ID: "f2", SourceRef: "notify", TargetRef: "end"},
		},
	}
	if _, err := w.h.defSvc.CreateDefinition(w.h.ctx, def); err != nil {
		t.Fatalf("create definition: %v", err)
	}
}

// start starts the process and runs whatever it queued, returning the start's
// error.
func (w collectionWorld) start(t *testing.T, key string, variables map[string]any) error {
	t.Helper()
	_, err := w.h.engine.StartProcess(w.h.ctx, w.h.projectID, key, variables)
	if err == nil {
		if err := w.h.jobSvc.ProcessPendingJobs(w.h.ctx); err != nil {
			t.Fatalf("process pending jobs: %v", err)
		}
	}
	return err
}

func TestAStepOverAListThatIsNotThereIsRefused(t *testing.T) {
	w := newCollectionWorld(t)
	w.notifyEach(t, "notify-misspelled", "supplers")

	err := w.start(t, "notify-misspelled", map[string]any{"suppliers": []any{"Acme", "Initech"}})
	if err == nil {
		t.Fatal("a step over a list the instance does not have started; it used to run once for nobody")
	}
	if !strings.Contains(err.Error(), "notify") || !strings.Contains(err.Error(), `"supplers"`) {
		t.Fatalf("the refusal does not name the step and the variable: %v", err)
	}
	if n := w.calls.Load(); n != 0 {
		t.Fatalf("the endpoint was called %d time(s) for a list that is not there", n)
	}
}

func TestAStepOverAVariableThatIsNotAListIsRefused(t *testing.T) {
	w := newCollectionWorld(t)
	w.notifyEach(t, "notify-not-a-list", "suppliers")

	err := w.start(t, "notify-not-a-list", map[string]any{"suppliers": "Acme"})
	if err == nil {
		t.Fatal("a step over a piece of text started; it used to run once with no item")
	}
	if !strings.Contains(err.Error(), "a piece of text") {
		t.Fatalf("the refusal does not say what the variable holds: %v", err)
	}
	if n := w.calls.Load(); n != 0 {
		t.Fatalf("the endpoint was called %d time(s) for a variable that is not a list", n)
	}
}

// Every parallel token is created before any runs, so a list as long as a
// million items exhausted memory exactly as a count of a million did.
func TestAStepOverAListLongerThanAStepMayRepeatIsRefused(t *testing.T) {
	w := newCollectionWorld(t)
	w.notifyEach(t, "notify-too-many", "suppliers")

	suppliers := make([]any, entities.MaxLoopCardinality+1)
	for i := range suppliers {
		suppliers[i] = fmt.Sprintf("supplier-%d", i)
	}
	err := w.start(t, "notify-too-many", map[string]any{"suppliers": suppliers})
	if err == nil {
		t.Fatalf("a step over %d items started", len(suppliers))
	}
	if !strings.Contains(err.Error(), fmt.Sprint(len(suppliers))) {
		t.Fatalf("the refusal does not say how many items there were: %v", err)
	}
	if n := w.calls.Load(); n != 0 {
		t.Fatalf("the endpoint was called %d time(s) for a list too long to run", n)
	}
}

// A model from Camunda names the list as an expression, "${suppliers}". It
// used to be looked up under that whole string, never found, and run once.
func TestAStepOverAListNamedAsAnExpressionRunsOnceForEachItem(t *testing.T) {
	w := newCollectionWorld(t)
	w.notifyEach(t, "notify-expression", "${suppliers}")

	if err := w.start(t, "notify-expression", map[string]any{"suppliers": []any{"Northwind", "Acme", "Initech"}}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if n := w.calls.Load(); n != 3 {
		t.Fatalf("the endpoint was called %d time(s) for a list of 3 named ${suppliers}", n)
	}
}
