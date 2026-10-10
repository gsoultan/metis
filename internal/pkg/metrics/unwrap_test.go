package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// The event stream sets a deadline on each write through
// http.ResponseController, which has to reach the connection through this
// wrapper. Without Unwrap it answered "not supported", and every stream in
// production ran with no write deadline at all.
func TestAWriteDeadlineReachesTheConnectionThroughTheRecorder(t *testing.T) {
	for _, path := range []string{"/api/v1/tasks", "/api/v1/events"} {
		t.Run(path, func(t *testing.T) {
			deadlineErr := make(chan error, 1)
			c := New(WithStreams("/api/v1/events"))
			server := httptest.NewServer(c.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				deadlineErr <- http.NewResponseController(w).SetWriteDeadline(time.Now().Add(time.Second))
			})))
			t.Cleanup(server.Close)

			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+path, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			_ = response.Body.Close()

			if err := <-deadlineErr; err != nil {
				t.Fatalf("setting a write deadline through the metrics wrapper failed: %v", err)
			}
		})
	}
}
