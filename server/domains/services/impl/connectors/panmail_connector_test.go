package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gsoultan/metis/internal/pkg/idempotency"
	panmail "github.com/gsoultan/panmail-sdk"
)

const panmailSendPath = "/panmail.v1.EmailService/SendEmail"

// panmailGateway is a fake gateway that records what it was sent and answers
// with the given status and body.
type panmailGateway struct {
	server   *httptest.Server
	requests atomic.Int32
	lastKey  atomic.Value
	lastBody atomic.Value
}

func newPanmailGateway(t *testing.T, status int, reply string) *panmailGateway {
	t.Helper()
	gateway := &panmailGateway{}
	gateway.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gateway.requests.Add(1)
		if r.URL.Path != panmailSendPath || r.Method != http.MethodPost {
			http.Error(w, "unexpected route "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		gateway.lastKey.Store(r.Header.Get("X-API-Key"))
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gateway.lastBody.Store(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(gateway.server.Close)
	return gateway
}

func (g *panmailGateway) config() map[string]any {
	return map[string]any{
		"url":         g.server.URL,
		"api_key":     "pm_test_key",
		"provider_id": "prov-1",
		"from":        "noreply@example.com",
	}
}

func (g *panmailGateway) body(t *testing.T) map[string]any {
	t.Helper()
	body, ok := g.lastBody.Load().(map[string]any)
	if !ok {
		t.Fatal("the gateway received no send")
	}
	return body
}

func panmailContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestPanmailSendsTheStepsMailAndReturnsTheMessageID(t *testing.T) {
	gateway := newPanmailGateway(t, http.StatusOK, `{"messageId":"msg-42","status":"EMAIL_EVENT_TYPE_PENDING"}`)
	connector := NewPanmailConnector(gateway.server.Client())

	ctx := idempotency.WithKey(panmailContext(t), "gobpm-abc")
	result, err := connector.Execute(ctx, gateway.config(), map[string]any{
		"to":      "ann@example.com, bob@example.com",
		"cc":      "carol@example.com",
		"subject": "Approval waiting",
		"body":    "Please review.",
		"html":    "<p>Please review.</p>",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result["status"] != "sent" || result["message_id"] != "msg-42" {
		t.Fatalf("result = %v, want status sent and message_id msg-42", result)
	}
	if got := gateway.lastKey.Load(); got != "pm_test_key" {
		t.Fatalf("X-API-Key = %v, want the configured key", got)
	}
	body := gateway.body(t)
	checks := map[string]any{
		"providerId":     "prov-1",
		"from":           "noreply@example.com",
		"subject":        "Approval waiting",
		"bodyText":       "Please review.",
		"bodyHtml":       "<p>Please review.</p>",
		"idempotencyKey": "gobpm-abc",
	}
	for key, want := range checks {
		if body[key] != want {
			t.Errorf("%s = %v, want %v", key, body[key], want)
		}
	}
	if to, _ := json.Marshal(body["to"]); string(to) != `["ann@example.com","bob@example.com"]` {
		t.Errorf("to = %s, want both recipients, trimmed", to)
	}
}

// A job retried after a timeout must not mail the person twice; the key is
// what lets the gateway recognise the repeat. Outside a job there is none.
func TestPanmailSendsNoIdempotencyKeyOutsideAJob(t *testing.T) {
	gateway := newPanmailGateway(t, http.StatusOK, `{"messageId":"msg-1"}`)
	connector := NewPanmailConnector(gateway.server.Client())

	if _, err := connector.Execute(panmailContext(t), gateway.config(), map[string]any{
		"to": "ann@example.com", "subject": "Hi", "body": "Hello",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if key, present := gateway.body(t)["idempotencyKey"]; present {
		t.Fatalf("idempotencyKey = %v, want none outside a job", key)
	}
}

func TestPanmailRefusesAnIncompleteStepBeforeCallingTheGateway(t *testing.T) {
	cases := []struct {
		name    string
		config  func(map[string]any)
		payload map[string]any
		want    string
	}{
		{"no api key", func(c map[string]any) { delete(c, "api_key") }, map[string]any{"to": "a@example.com", "subject": "s", "body": "b"}, "'api_key'"},
		{"no provider", func(c map[string]any) { c["provider_id"] = " " }, map[string]any{"to": "a@example.com", "subject": "s", "body": "b"}, "'provider_id'"},
		{"no recipient", nil, map[string]any{"to": " , ", "subject": "s", "body": "b"}, "'to'"},
		{"no subject", nil, map[string]any{"to": "a@example.com", "body": "b"}, "'subject'"},
		{"no body", nil, map[string]any{"to": "a@example.com", "subject": "s"}, "'body'"},
		{"line break in to", nil, map[string]any{"to": "a@example.com\r\nBcc: x@example.net", "subject": "s", "body": "b"}, "line break"},
		{"line break in from", func(c map[string]any) { c["from"] = "a@example.com\nBcc: x@example.net" }, map[string]any{"to": "a@example.com", "subject": "s", "body": "b"}, "line break"},
		{"plain http to a remote gateway", func(c map[string]any) { c["url"] = "http://mail.example.com" }, map[string]any{"to": "a@example.com", "subject": "s", "body": "b"}, "cleartext"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gateway := newPanmailGateway(t, http.StatusOK, `{"messageId":"m"}`)
			config := gateway.config()
			if tc.config != nil {
				tc.config(config)
			}
			_, err := NewPanmailConnector(gateway.server.Client()).Execute(panmailContext(t), config, tc.payload)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %s", err, tc.want)
			}
			if n := gateway.requests.Load(); n != 0 {
				t.Fatalf("the gateway was called %d times for a step that could not be sent", n)
			}
		})
	}
}

// A line break in the subject is folded: it never reaches a header as one.
func TestPanmailFoldsALineBreakInTheSubject(t *testing.T) {
	gateway := newPanmailGateway(t, http.StatusOK, `{"messageId":"m"}`)
	if _, err := NewPanmailConnector(gateway.server.Client()).Execute(panmailContext(t), gateway.config(), map[string]any{
		"to": "a@example.com", "subject": "Invoice\r\nBcc: x@example.net", "body": "b",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := gateway.body(t)["subject"]; got != "Invoice Bcc: x@example.net" {
		t.Fatalf("subject = %q, want the line break folded to a space", got)
	}
}

func TestPanmailExplainsTheRefusalsAnAdministratorCanFix(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reply  string
		want   string
		target any
	}{
		{"bad key", http.StatusUnauthorized, `{"code":"unauthenticated","message":"bad key"}`, "email:send scope", new(*panmail.AuthError)},
		{"suppressed", http.StatusBadRequest, `{"code":"failed_precondition","message":"suppressed"}`, "lift the suppression", new(*panmail.SuppressedRecipientError)},
		{"wrong provider", http.StatusBadRequest, `{"code":"invalid_argument","message":"unknown provider"}`, "check the provider id", new(*panmail.APIError)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gateway := newPanmailGateway(t, tc.status, tc.reply)
			_, err := NewPanmailConnector(gateway.server.Client()).Execute(panmailContext(t), gateway.config(), map[string]any{
				"to": "a@example.com", "subject": "s", "body": "b",
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want the hint %q", err, tc.want)
			}
			if !errors.As(err, tc.target) {
				t.Fatalf("err = %v, want the SDK's typed error kept for errors.As", err)
			}
			if strings.Contains(err.Error(), "pm_test_key") {
				t.Fatalf("the error carries the API key: %v", err)
			}
		})
	}
}
