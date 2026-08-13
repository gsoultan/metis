package bpmn_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gsoultan/gobpm/server/domains/entities"
)

// "Do this once for each item in a list."
//
// The engine implements it, the designer can configure it, and nothing ran it:
// the only test that named multiInstanceType before this one checked that the
// field survived a trip to protobuf.
//
// Two things have to be true for the feature to be worth having. It has to run
// once per item, and each run has to be able to tell which item it got.

func TestMultiInstance_RunsOnceForEachItemInTheCollection(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer api.Close()

	h := newServiceTaskHarness(t)
	h.run(t, entities.Node{
		ID:                "notify",
		Name:              "Notify each supplier",
		Type:              entities.ServiceTask,
		MultiInstanceType: "parallel",
		Collection:        "suppliers",
		ElementVariable:   "supplier",
		Properties: map[string]any{
			"http_url":       api.URL,
			"http_method":    "POST",
			"input_supplier": "supplier_name",
		},
	}, map[string]any{
		"suppliers": []any{"Northwind", "Acme", "Initech"},
	})

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 3 {
		t.Fatalf("the endpoint was called %d times for a collection of 3", len(bodies))
	}

	// Each call should carry its own supplier. Without that, "once per item" is
	// only "three times", and the task cannot act on the item it was given.
	got := map[string]bool{}
	for _, body := range bodies {
		if name, ok := body["supplier_name"].(string); ok {
			got[name] = true
		}
	}
	for _, want := range []string{"Northwind", "Acme", "Initech"} {
		if !got[want] {
			t.Errorf("no iteration was given %q — each one saw the same variables, so none of them knows which item it has (got %v)", want, got)
		}
	}
}

// A count with no collection is the other way to say how many times.
func TestMultiInstance_RunsLoopCardinalityTimes(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer api.Close()

	h := newServiceTaskHarness(t)
	h.run(t, entities.Node{
		ID:                "ping",
		Name:              "Ping three times",
		Type:              entities.ServiceTask,
		MultiInstanceType: "parallel",
		LoopCardinality:   3,
		Properties:        map[string]any{"http_url": api.URL, "http_method": "POST"},
	}, map[string]any{})

	mu.Lock()
	defer mu.Unlock()
	if calls != 3 {
		t.Errorf("the endpoint was called %d times for a loop cardinality of 3", calls)
	}
}

// An empty collection means there is nothing to do, not that the process should
// stop: the step is skipped and the process carries on.
func TestMultiInstance_WithAnEmptyCollectionRunsOnceAndProceeds(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer api.Close()

	h := newServiceTaskHarness(t)
	instance := h.run(t, entities.Node{
		ID:                "notify",
		Type:              entities.ServiceTask,
		MultiInstanceType: "parallel",
		Collection:        "suppliers",
		Properties:        map[string]any{"http_url": api.URL, "http_method": "POST"},
	}, map[string]any{"suppliers": []any{}})

	if instance.Status != entities.ProcessCompleted {
		t.Errorf("instance is %q; an empty collection should not leave the process stuck", instance.Status)
	}
}

// The bookkeeping the engine keeps while iterating is its own business and
// should not be left in the process's variables, where it shows up in the
// instance view and in anything a later step reads.
func TestMultiInstance_DoesNotLeaveItsBookkeepingBehind(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer api.Close()

	h := newServiceTaskHarness(t)
	instance := h.run(t, entities.Node{
		ID:                "notify",
		Type:              entities.ServiceTask,
		MultiInstanceType: "parallel",
		Collection:        "suppliers",
		ElementVariable:   "supplier",
		Properties:        map[string]any{"http_url": api.URL, "http_method": "POST"},
	}, map[string]any{"suppliers": []any{"Northwind", "Acme"}})

	for name := range instance.Variables {
		if len(name) > 4 && name[:4] == "_mi_" {
			t.Errorf("the instance still carries %q after the iterations finished", name)
		}
	}
}

// Sequential means one at a time, in order — not one and then silence.
func TestMultiInstance_SequentialRunsEveryItemAndFinishes(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		if name, ok := body["supplier_name"].(string); ok {
			seen = append(seen, name)
		} else {
			seen = append(seen, "(no supplier)")
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer api.Close()

	h := newServiceTaskHarness(t)
	instance := h.run(t, entities.Node{
		ID:                "notify",
		Name:              "Notify each supplier in turn",
		Type:              entities.ServiceTask,
		MultiInstanceType: "sequential",
		Collection:        "suppliers",
		ElementVariable:   "supplier",
		Properties: map[string]any{
			"http_url":       api.URL,
			"http_method":    "POST",
			"input_supplier": "supplier_name",
		},
	}, map[string]any{"suppliers": []any{"Northwind", "Acme", "Initech"}})

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 {
		t.Errorf("ran %d of 3 iterations (%v) — the ones after the first were never started", len(seen), seen)
	}
	if instance.Status != entities.ProcessCompleted {
		t.Errorf("instance is %q; it should finish once every item has been handled", instance.Status)
	}
}
