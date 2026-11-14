package impl

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// A Discord step with no message used to post "No content provided" and
// report it sent.
func TestDiscordRefusesAMessageWithNothingInIt(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	var posted atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		posted.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	_, err := (&DiscordMessageExecutor{}).Execute(t.Context(),
		map[string]any{"webhook_url": server.URL}, map[string]any{"amount": 500})
	if err == nil || !strings.Contains(err.Error(), "'content'") {
		t.Errorf("error = %v, want one naming the missing content", err)
	}
	if posted.Load() != 0 {
		t.Error("an empty message was posted to Discord")
	}

	if _, err := (&DiscordMessageExecutor{}).Execute(t.Context(),
		map[string]any{"webhook_url": server.URL}, map[string]any{"text": "PO-17 approved"}); err != nil {
		t.Fatalf("a message given as text: %v", err)
	}
	if posted.Load() != 1 {
		t.Errorf("posted %d messages, want the one with text", posted.Load())
	}
}
