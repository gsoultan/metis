package connectors

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// teamsWebhook records what it was sent and answers with status.
func teamsWebhook(t *testing.T, status int) (*httptest.Server, *map[string]any) {
	t.Helper()
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	received := map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &received); err != nil {
			t.Errorf("the webhook was not sent JSON: %q", raw)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server, &received
}

// A Workflows webhook reads only an Adaptive Card attachment, and answers 202.
func TestTeamsSendsAnAdaptiveCardAWorkflowsWebhookReads(t *testing.T) {
	server, received := teamsWebhook(t, http.StatusAccepted)

	_, err := NewTeamsConnector().Execute(t.Context(),
		map[string]any{"webhook_url": server.URL},
		map[string]any{"title": "Purchase request", "text": "Approve PO-17 for 4,000 EUR"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	raw, _ := json.Marshal(*received)
	sent := string(raw)
	for _, want := range []string{
		`"type":"message"`, `"contentType":"application/vnd.microsoft.card.adaptive"`,
		`"type":"AdaptiveCard"`, `"text":"Purchase request"`, `"text":"Approve PO-17 for 4,000 EUR"`,
	} {
		if !strings.Contains(sent, want) {
			t.Errorf("the card is missing %s:\n%s", want, sent)
		}
	}
}

func TestTeamsRefusesWhatItCannotSend(t *testing.T) {
	server, _ := teamsWebhook(t, http.StatusOK)
	testCases := []struct {
		name    string
		config  map[string]any
		payload map[string]any
		want    string
	}{
		{"no message", map[string]any{"webhook_url": server.URL}, map[string]any{"amount": 500}, "'text'"},
		{"no webhook", map[string]any{}, map[string]any{"text": "hi"}, "'webhook_url'"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewTeamsConnector().Execute(t.Context(), tc.config, tc.payload)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want one naming %s", err, tc.want)
			}
		})
	}
}

func TestTeamsReadsTheMessageUnderEitherName(t *testing.T) {
	server, received := teamsWebhook(t, http.StatusOK)
	if _, err := NewTeamsConnector().Execute(t.Context(),
		map[string]any{"webhook_url": server.URL}, map[string]any{"content": "From content"}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	raw, _ := json.Marshal(*received)
	if !strings.Contains(string(raw), "From content") {
		t.Errorf("the message given as content was not sent: %s", raw)
	}
}

func TestTeamsReportsARefusal(t *testing.T) {
	server, _ := teamsWebhook(t, http.StatusBadRequest)
	_, err := NewTeamsConnector().Execute(t.Context(),
		map[string]any{"webhook_url": server.URL}, map[string]any{"text": "hi"})
	if err == nil {
		t.Fatal("a 400 from Teams was reported as sent")
	}
}
