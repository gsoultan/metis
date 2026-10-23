package impl

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// countedValue counts how often an event carrying it is encoded.
type countedValue struct{ encodes *atomic.Int32 }

func (c countedValue) MarshalJSON() ([]byte, error) {
	c.encodes.Add(1)
	return []byte(`"counted"`), nil
}

func countedEvent() (entities.ProcessEvent, *atomic.Int32) {
	var encodes atomic.Int32
	return entities.ProcessEvent{
		Type:      entities.EventNodeReached,
		Timestamp: 1,
		Variables: map[string]any{"amount": countedValue{&encodes}},
	}, &encodes
}

func scopedContext(scope entities.SSEScope) context.Context {
	return entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: scope.Organization.String()})
}

// An event is encoded only when somebody could receive it, and then once.
func TestAnEventIsEncodedOnlyForSomebodyWhoCanReceiveIt(t *testing.T) {
	otherOrganization := entities.SSEScope{Organization: uuid.New()}
	tests := []struct {
		name        string
		clients     []entities.SSEScope
		bus         bool
		wantEncodes int32
	}{
		{name: "nobody connected and no bus", wantEncodes: 0},
		{name: "only another organization connected", clients: []entities.SSEScope{otherOrganization}, wantEncodes: 0},
		{name: "a browser of this organization", clients: []entities.SSEScope{testScope}, wantEncodes: 1},
		{name: "two browsers of this organization", clients: []entities.SSEScope{testScope, testScope}, wantEncodes: 1},
		{name: "nobody here, but other replicas on the bus", bus: true, wantEncodes: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := NewSSEObserver()
			for _, scope := range tt.clients {
				o.AddClient(scope)
			}
			var published atomic.Int32
			if tt.bus {
				o.PublishVia(func(entities.SSEScope, string) { published.Add(1) })
			}
			event, encodes := countedEvent()

			o.OnEvent(scopedContext(testScope), event)

			if got := encodes.Load(); got != tt.wantEncodes {
				t.Fatalf("encoded %d times, want %d", got, tt.wantEncodes)
			}
			if tt.bus && published.Load() != 1 {
				t.Fatalf("published %d times, want 1", published.Load())
			}
		})
	}
}

// What a browser is sent is the event's own encoding, framed once: not the
// encoding encoded again.
func TestABrowserIsSentTheEventAsItEncodes(t *testing.T) {
	o := NewSSEObserver()
	inScope := o.AddClient(testScope)
	elsewhere := o.AddClient(entities.SSEScope{Organization: uuid.New()})
	event := entities.ProcessEvent{Type: entities.EventTaskCreated, Timestamp: 7, Assignee: "dita"}

	o.OnEvent(scopedContext(testScope), event)

	want, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	select {
	case got := <-inScope:
		if got != "data: "+string(want)+"\n\n" {
			t.Fatalf("sent %q, want the event framed as one data line", got)
		}
	default:
		t.Fatal("the browser in the event's organization was sent nothing")
	}
	select {
	case got := <-elsewhere:
		t.Fatalf("another organization's browser was sent %q", got)
	default:
	}
}

// BenchmarkOnEventWithNobodyListening is what every step of every instance
// paid the event stream on a replica with no browser open.
func BenchmarkOnEventWithNobodyListening(b *testing.B) {
	benchmarkOnEvent(b, NewSSEObserver())
}

// BenchmarkOnEventToOneBrowser is the same event delivered to one browser.
func BenchmarkOnEventToOneBrowser(b *testing.B) {
	o := NewSSEObserver()
	ch := o.AddClient(testScope)
	go func() {
		for range ch {
		}
	}()
	benchmarkOnEvent(b, o)
}

func benchmarkOnEvent(b *testing.B, o *SSEObserver) {
	ctx := scopedContext(testScope)
	event := entities.ProcessEvent{
		Type:      entities.EventNodeReached,
		Timestamp: 1,
		Instance:  &entities.ProcessInstance{ID: uuid.New(), Project: &entities.Project{ID: uuid.New()}, Status: "active"},
		Node:      &entities.Node{ID: "review", Name: "Review the order", Type: entities.UserTask},
		Variables: map[string]any{"amount": 1200.5, "customer": "Northwind Ltd", "lines": []any{1, 2, 3}},
	}
	b.ReportAllocs()
	for b.Loop() {
		o.OnEvent(ctx, event)
	}
}
