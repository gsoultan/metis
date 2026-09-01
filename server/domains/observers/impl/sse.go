package impl

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gsoultan/metis/server/domains/entities"
)

type SSEObserver struct {
	mu      sync.RWMutex
	clients map[chan string]bool

	// publish hands an encoded event to the shared bus so replicas other than
	// this one can deliver it. Nil on a single-replica installation and in
	// tests, where local delivery is the whole story — a nil check is cheaper
	// than a null-object indirection on a path that runs per event.
	publish func(payload string)
}

func NewSSEObserver() *SSEObserver {
	return &SSEObserver{
		clients: make(map[chan string]bool),
	}
}

// PublishVia gives the observer a shared bus to put events on.
//
// Delivery to this process's own clients does not go through the bus: it
// happens inline, so a browser connected here sees its own replica's events at
// the speed it always did. The bus exists only to reach browsers connected
// somewhere else.
func (o *SSEObserver) PublishVia(publish func(payload string)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.publish = publish
}

func (o *SSEObserver) OnEvent(ctx context.Context, event entities.ProcessEvent) {
	o.Broadcast(event)
}

func (o *SSEObserver) Broadcast(data any) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}
	payload := string(jsonData)

	o.deliverLocally(payload)

	o.mu.RLock()
	publish := o.publish
	o.mu.RUnlock()
	if publish != nil {
		publish(payload)
	}
}

// DeliverFromPeer delivers an event another replica produced. It does not
// re-publish: the bus is where this came from, and putting it back would make
// every event circulate forever.
func (o *SSEObserver) DeliverFromPeer(payload string) {
	o.deliverLocally(payload)
}

// deliverLocally sends one encoded payload to the browsers connected here.
//
// A client whose buffer is full is skipped rather than waited for. That is
// deliberate and predates the bus: one browser on a slow connection must not
// hold up delivery to every other, and the UI treats an event as a hint to
// refetch rather than as data, so a dropped one costs a stale list until the
// next refetch — not a lost update.
func (o *SSEObserver) deliverLocally(payload string) {
	o.mu.RLock()
	defer o.mu.RUnlock()

	msg := fmt.Sprintf("data: %s\n\n", payload)
	for clientChan := range o.clients {
		select {
		case clientChan <- msg:
		default:
			// Client slow or disconnected
		}
	}
}

func (o *SSEObserver) AddClient() chan string {
	o.mu.Lock()
	defer o.mu.Unlock()
	ch := make(chan string, 10)
	o.clients[ch] = true
	return ch
}

func (o *SSEObserver) RemoveClient(ch chan string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	delete(o.clients, ch)
	close(ch)
}
