package impl

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// mailGateway is a fake Panmail gateway that keeps every send it is given.
type mailGateway struct {
	server *httptest.Server
	mu     sync.Mutex
	sends  []map[string]any
	keys   []string
}

func newMailGateway(t *testing.T, status int, reply string) *mailGateway {
	t.Helper()
	gateway := &mailGateway{}
	gateway.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/panmail.v1.EmailService/SendEmail" || r.Method != http.MethodPost {
			http.Error(w, "unexpected route "+r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		gateway.mu.Lock()
		gateway.sends = append(gateway.sends, body)
		gateway.keys = append(gateway.keys, r.Header.Get("X-API-Key"))
		gateway.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(gateway.server.Close)
	return gateway
}

const acceptedReply = `{"messageId":"msg-1","status":"EMAIL_EVENT_TYPE_PENDING"}`

func (g *mailGateway) settings() EmailSettings {
	return EmailSettings{
		// An SMTP server is set too: once a gateway is chosen it must not be
		// the one the mail goes through.
		Host: "smtp.example.com",
		From: "noreply@example.com",
		Panmail: PanmailSettings{
			URL:        g.server.URL,
			APIKey:     "pm_test_key",
			ProviderID: "prov-1",
		},
	}
}

func (g *mailGateway) received() []map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.sends)
}

func refuseSMTP(t *testing.T) EmailOption {
	return WithMailSender(func(context.Context, string, smtp.Auth, string, []string, []byte) error {
		t.Error("the mail went to the SMTP server although a Panmail gateway is chosen")
		return nil
	})
}

func addressOf(address string) AddressLookup {
	return func(context.Context, string) (string, error) { return address, nil }
}

func TestANotificationGoesThroughThePanmailGatewayWhenOneIsChosen(t *testing.T) {
	gateway := newMailGateway(t, http.StatusOK, acceptedReply)
	channel := NewEmailNotificationChannel(gateway.settings(), addressOf("ollie@example.com"),
		WithPanmailHTTPClient(gateway.server.Client()), refuseSMTP(t))
	if channel == nil {
		t.Fatal("complete Panmail settings produced no channel")
	}

	n := aNotification()
	n.Title = "Approve order\nBcc: attacker@example.net"
	if err := channel.Deliver(t.Context(), n); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	sends := gateway.received()
	if len(sends) != 1 {
		t.Fatalf("the gateway got %d sends, want 1", len(sends))
	}
	got := sends[0]
	if got["providerId"] != "prov-1" || got["from"] != "noreply@example.com" {
		t.Errorf("provider and sender: %v, %v", got["providerId"], got["from"])
	}
	if to, _ := got["to"].([]any); len(to) != 1 || to[0] != "ollie@example.com" {
		t.Errorf("recipients: %v, want the person alone", got["to"])
	}
	if got["subject"] != "Approve order Bcc: attacker@example.net" {
		t.Errorf("subject %q: a line break in the title must be folded", got["subject"])
	}
	text, _ := got["bodyText"].(string)
	if !strings.Contains(text, "Operations approve") || !strings.Contains(text, n.Link) {
		t.Errorf("the body lacks the message or the link:\n%s", text)
	}
	if got["idempotencyKey"] != "metis-notification-"+n.ID.String() {
		t.Errorf("idempotency key %v, want one named after the notification", got["idempotencyKey"])
	}
	if gateway.keys[0] != "pm_test_key" {
		t.Errorf("the gateway was not given the API key")
	}
}

// The gateway is asked again when a send's outcome is unknown, so the key has
// to name the notification rather than the attempt: the same notification
// mailed twice is one message to the gateway.
func TestTheSameNotificationCarriesTheSameKeyEachTime(t *testing.T) {
	gateway := newMailGateway(t, http.StatusOK, acceptedReply)
	channel := NewEmailNotificationChannel(gateway.settings(), addressOf("ollie@example.com"),
		WithPanmailHTTPClient(gateway.server.Client()))
	n := aNotification()
	for range 2 {
		if err := channel.Deliver(t.Context(), n); err != nil {
			t.Fatalf("deliver: %v", err)
		}
	}
	sends := gateway.received()
	if len(sends) != 2 || sends[0]["idempotencyKey"] != sends[1]["idempotencyKey"] {
		t.Fatalf("keys differ between deliveries of one notification: %v", sends)
	}
}

func TestAGatewayRefusalSaysWhichSettingToFix(t *testing.T) {
	gateway := newMailGateway(t, http.StatusUnauthorized, `{"code":"unauthenticated","message":"bad key"}`)
	channel := NewEmailNotificationChannel(gateway.settings(), addressOf("ollie@example.com"),
		WithPanmailHTTPClient(gateway.server.Client()))
	err := channel.Deliver(t.Context(), aNotification())
	if err == nil {
		t.Fatal("a refused send was reported as delivered")
	}
	if !strings.Contains(err.Error(), "API key") || !strings.Contains(err.Error(), "ollie@example.com") {
		t.Fatalf("the error does not say who or what to fix: %v", err)
	}
}

// A gateway chosen and half set up sends nothing, rather than mail going out
// through the SMTP server the administrator chose against.
func TestAHalfSetUpGatewayIsReportedNotReplacedBySMTP(t *testing.T) {
	settings := EmailSettings{
		Host: "smtp.example.com", From: "noreply@example.com",
		Panmail: PanmailSettings{URL: "https://mail.example.com"},
	}
	if settings.Configured() {
		t.Fatal("a gateway with no key and no provider counted as configured")
	}
	if got, want := settings.Missing(), []string{"an API key", "a provider id"}; !slices.Equal(got, want) {
		t.Errorf("missing %v, want %v", got, want)
	}
	if settings.Transport() != MailTransportPanmail {
		t.Errorf("transport %q, want panmail", settings.Transport())
	}
	if NewEmailNotificationChannel(settings, addressOf("ollie@example.com")) != nil {
		t.Error("a half set-up gateway produced a channel")
	}

	smtpOnly := EmailSettings{Host: "smtp.example.com", From: "noreply@example.com"}
	if !smtpOnly.Configured() || smtpOnly.Transport() != MailTransportSMTP {
		t.Error("SMTP settings with no gateway stopped meaning SMTP")
	}
	if got := (EmailSettings{Panmail: settings.Panmail}).Missing(); !slices.Contains(got, "a sender address") {
		t.Errorf("a gateway with no sender address: missing %v", got)
	}
}

// The API key is a tenant-wide sending credential: a remote gateway over
// plain http would put it on the wire in the clear.
func TestARemoteGatewayOverPlainHTTPIsRefused(t *testing.T) {
	settings := EmailSettings{
		From: "noreply@example.com",
		Panmail: PanmailSettings{
			URL: "http://mail.example.com", APIKey: "pm_test_key", ProviderID: "prov-1",
		},
	}
	if NewEmailNotificationChannel(settings, addressOf("ollie@example.com")) != nil {
		t.Fatal("a remote gateway over plain http produced a channel")
	}
}

func TestATaskLinkIsMailedThroughPanmailWithAKeyNamedAfterTheLink(t *testing.T) {
	gateway := newMailGateway(t, http.StatusOK, acceptedReply)
	mail := NewTaskLinkMailer(gateway.settings(), "https://metis.example.com", DeliverNow,
		WithPanmailHTTPClient(gateway.server.Client()), refuseSMTP(t))
	link := servicecontracts.IssuedTaskLink{
		Person:    entities.Person{ID: uuid.New(), Username: "ada", DisplayName: "Ada", Email: "ada@example.com"},
		TaskID:    uuid.New(),
		Token:     "mtl_secret",
		ExpiresAt: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC),
	}
	event := entities.ProcessEvent{Node: &entities.Node{Name: "Approve"}}
	mail(t.Context(), link, event)
	mail(t.Context(), link, event)

	sends := gateway.received()
	if len(sends) != 2 {
		t.Fatalf("the gateway got %d sends, want 2", len(sends))
	}
	if to, _ := sends[0]["to"].([]any); len(to) != 1 || to[0] != "ada@example.com" {
		t.Errorf("recipients: %v, want ada's address alone", sends[0]["to"])
	}
	if text, _ := sends[0]["bodyText"].(string); !strings.Contains(text, "https://metis.example.com/p/t#mtl_secret") {
		t.Errorf("the mail does not carry the link:\n%s", text)
	}
	key, _ := sends[0]["idempotencyKey"].(string)
	if key == "" || key != sends[1]["idempotencyKey"] {
		t.Errorf("keys %v and %v: one link is one message", key, sends[1]["idempotencyKey"])
	}
	if strings.Contains(key, "mtl_secret") {
		t.Errorf("the key %q carries the link's token", key)
	}
}
