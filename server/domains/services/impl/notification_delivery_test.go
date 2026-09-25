package impl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// allowLoopbackEgress opts a test into contacting private addresses.
//
// Outbound HTTP is guarded by an SSRF egress policy that blocks loopback by
// default, and httptest servers bind to 127.0.0.1. A notification webhook goes
// through the same shared client as every other outbound call, so it is subject
// to the same policy — which is the point, and why the test has to say so.
func allowLoopbackEgress(t *testing.T) {
	t.Helper()
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
}

// Delivering a notification out of the application.
//
// The notification centre is only read by somebody already looking at it, and
// the point of telling people a task is theirs is to reach them when they are
// not.

type storingNotifier struct {
	stored []entities.Notification
	err    error
}

func (s *storingNotifier) Send(_ context.Context, n entities.Notification) error {
	if s.err != nil {
		return s.err
	}
	s.stored = append(s.stored, n)
	return nil
}
func (s *storingNotifier) ListByUser(context.Context, string) ([]entities.Notification, error) {
	return nil, nil
}
func (s *storingNotifier) MarkAsRead(context.Context, uuid.UUID) error { return nil }
func (s *storingNotifier) MarkAllAsRead(context.Context, string) error { return nil }
func (s *storingNotifier) Delete(context.Context, uuid.UUID) error     { return nil }

type countingChannel struct {
	delivered int
	err       error
}

func (c *countingChannel) Name() string { return "counting" }
func (c *countingChannel) Deliver(context.Context, entities.Notification) error {
	c.delivered++
	return c.err
}

func aNotification() entities.Notification {
	return entities.Notification{
		ID:       uuid.New(),
		User:     &entities.User{Username: "ollie"},
		Type:     entities.NotificationTaskAssignment,
		Title:    "A task is waiting for you",
		Message:  `"Operations approve" is waiting for you in Quotation approval.`,
		Link:     "/tasks?instance=" + uuid.New().String(),
		Project:  &entities.Project{ID: uuid.New()},
		Instance: &entities.ProcessInstance{ID: uuid.New()},
	}
}

func TestANotificationIsStoredBeforeItIsDelivered(t *testing.T) {
	store := &storingNotifier{}
	channel := &countingChannel{}
	svc := NewDeliveringNotificationService(store, DeliverNow, channel)

	if err := svc.Send(context.Background(), aNotification()); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(store.stored) != 1 {
		t.Fatalf("the notification was not stored (%d stored)", len(store.stored))
	}
	if channel.delivered != 1 {
		t.Fatalf("the notification was not delivered (%d delivered)", channel.delivered)
	}
}

func TestAChannelThatFailsDoesNotLoseTheNotification(t *testing.T) {
	// The in-app centre is the record. An SMTP server that is down must not
	// also cost somebody the one place the notification was guaranteed to be.
	store := &storingNotifier{}
	svc := NewDeliveringNotificationService(store, DeliverNow, &countingChannel{err: errors.New("smtp is down")})

	if err := svc.Send(context.Background(), aNotification()); err != nil {
		t.Fatalf("a failed channel failed the whole notification: %v", err)
	}
	if len(store.stored) != 1 {
		t.Fatalf("the notification was not stored (%d stored)", len(store.stored))
	}
}

func TestAStoreThatFailsIsStillAFailure(t *testing.T) {
	// The other direction: if the record itself did not happen, the caller has
	// to know. Delivery is the extra, not the point.
	store := &storingNotifier{err: errors.New("the database is gone")}
	channel := &countingChannel{}
	svc := NewDeliveringNotificationService(store, DeliverNow, channel)

	if err := svc.Send(context.Background(), aNotification()); err == nil {
		t.Fatal("a notification that was never stored reported success")
	}
	if channel.delivered != 0 {
		t.Fatalf("a notification that was never stored was delivered anyway (%d)", channel.delivered)
	}
}

func TestConfiguringNothingWrapsNothing(t *testing.T) {
	store := &storingNotifier{}
	if svc := NewDeliveringNotificationService(store, DeliverNow); svc != store {
		t.Fatal("an installation that configured no channels should get the service it passed in")
	}
	// "Not configured" is what the constructors return, and it has to be a
	// real nil rather than a typed nil pointer hiding in an interface —
	// otherwise the wrapper keeps it and the first delivery panics.
	if svc := NewDeliveringNotificationService(store, DeliverNow, NewWebhookNotificationChannel("")); svc != store {
		t.Fatal("an unconfigured webhook channel was kept and will panic on delivery")
	}
}

func TestTheWebhookPostsWhoItIsForAndWhatItSays(t *testing.T) {
	allowLoopbackEgress(t)

	var got webhookPayload
	var contentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notification := aNotification()
	channel := NewWebhookNotificationChannel(server.URL)
	if channel == nil {
		t.Fatal("a configured url produced no channel")
	}
	if err := channel.Deliver(context.Background(), notification); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	if contentType != "application/json" {
		t.Errorf("content type: got %q", contentType)
	}
	if got.Recipient != "ollie" {
		t.Errorf("recipient: got %q, want ollie", got.Recipient)
	}
	if got.Title != notification.Title || got.Message != notification.Message {
		t.Errorf("the payload does not carry what the notification said: %+v", got)
	}
	if got.Instance != notification.Instance.ID.String() {
		t.Errorf("the payload does not identify the instance: %+v", got)
	}
}

func TestAWebhookThatRefusesIsNotReportedAsDelivered(t *testing.T) {
	// A receiver answering 500 has not taken it, and treating that as delivered
	// is the same mistake as publishing without confirms.
	allowLoopbackEgress(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	err := NewWebhookNotificationChannel(server.URL).Deliver(context.Background(), aNotification())
	if err == nil {
		t.Fatal("a webhook that answered 500 was reported as delivered")
	}
}

func TestNoWebhookUrlMeansNoWebhookChannel(t *testing.T) {
	if NewWebhookNotificationChannel("") != nil {
		t.Error("an empty url produced a channel")
	}
	if NewWebhookNotificationChannel("   ") != nil {
		t.Error("a blank url produced a channel")
	}
}

func TestTheEmailGoesToThePersonNotTheSender(t *testing.T) {
	// The connector next door addressed the envelope to itself for a while and
	// every message went to the noreply mailbox. Once is enough.
	var gotTo []string
	var gotFrom string
	var gotMsg string

	channel := NewEmailNotificationChannel(
		EmailSettings{Host: "smtp.example.com", From: "noreply@example.com"},
		func(context.Context, string) (string, error) { return "ollie@example.com", nil },
		WithMailSender(func(_ context.Context, _ string, _ smtp.Auth, from string, to []string, msg []byte) error {
			gotFrom, gotTo, gotMsg = from, to, string(msg)
			return nil
		}),
	)
	if channel == nil {
		t.Fatal("configured settings produced no channel")
	}

	if err := channel.Deliver(context.Background(), aNotification()); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	if len(gotTo) != 1 || gotTo[0] != "ollie@example.com" {
		t.Fatalf("envelope recipients: got %v, want [ollie@example.com]", gotTo)
	}
	if gotFrom != "noreply@example.com" {
		t.Errorf("envelope sender: got %q", gotFrom)
	}
	if !strings.Contains(gotMsg, "Subject: A task is waiting for you") {
		t.Errorf("the message carries no subject:\n%s", gotMsg)
	}
	if !strings.Contains(gotMsg, "Operations approve") {
		t.Errorf("the message carries no body:\n%s", gotMsg)
	}
}

func TestSomebodyWithNoAddressIsSimplyNotEmailed(t *testing.T) {
	// Plenty of accounts have no address. A notification for one of them is an
	// in-app notification, not a failure worth logging every time.
	sent := 0
	channel := NewEmailNotificationChannel(
		EmailSettings{Host: "smtp.example.com", From: "noreply@example.com"},
		func(context.Context, string) (string, error) { return "", nil },
		WithMailSender(func(context.Context, string, smtp.Auth, string, []string, []byte) error {
			sent++
			return nil
		}),
	)

	if err := channel.Deliver(context.Background(), aNotification()); err != nil {
		t.Fatalf("an account with no address produced an error: %v", err)
	}
	if sent != 0 {
		t.Fatalf("mail was sent to nobody (%d)", sent)
	}
}

func TestAnAddressThatCannotBeLookedUpIsAnError(t *testing.T) {
	channel := NewEmailNotificationChannel(
		EmailSettings{Host: "smtp.example.com", From: "noreply@example.com"},
		func(context.Context, string) (string, error) { return "", errors.New("no such user") },
		WithMailSender(func(context.Context, string, smtp.Auth, string, []string, []byte) error { return nil }),
	)

	if err := channel.Deliver(context.Background(), aNotification()); err == nil {
		t.Fatal("a lookup that failed was treated as nobody to email")
	}
}

func TestMailThatIsNotConfiguredProducesNoChannel(t *testing.T) {
	lookup := func(context.Context, string) (string, error) { return "x@example.com", nil }
	if NewEmailNotificationChannel(EmailSettings{From: "a@b"}, lookup) != nil {
		t.Error("settings with no host produced a channel")
	}
	if NewEmailNotificationChannel(EmailSettings{Host: "smtp"}, lookup) != nil {
		t.Error("settings with no from address produced a channel")
	}
	if NewEmailNotificationChannel(EmailSettings{Host: "smtp", From: "a@b"}, nil) != nil {
		t.Error("no way to look up an address produced a channel")
	}
}
