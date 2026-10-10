package impl_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
)

// TestASlowReceiverDoesNotPileUpSends: every event started a goroutine per
// endpoint holding its payload for up to the send timeout, with no bound. A
// receiver that stalled and a process that raised events quickly grew them
// until the process ran out of memory.
func TestASlowReceiverDoesNotPileUpSends(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })

	now := func(_ context.Context, fn func()) { fn() }
	observer := impl.NewWebhookObserver([]string{server.URL}, now)
	event := entities.ProcessEvent{Type: entities.EventTaskCompleted, Timestamp: time.Now().Unix()}

	before := runtime.NumGoroutine()
	for range 2000 {
		observer.OnEvent(t.Context(), event)
	}
	time.Sleep(200 * time.Millisecond)

	// Each send in flight costs a few goroutines (client, transport, server
	// side); 2000 unbounded sends cost thousands.
	if grew := runtime.NumGoroutine() - before; grew > 1000 {
		t.Fatalf("2000 events against a stalled receiver left %d more goroutines", grew)
	}
}
