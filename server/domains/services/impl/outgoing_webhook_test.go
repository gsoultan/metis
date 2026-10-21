package impl

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/webhooksig"
	"github.com/gsoultan/metis/server/domains/entities"
)

// An outgoing webhook's address is typed by a person and dialled by the
// server, so every way it could point somewhere it should not is refused when
// it is saved, in words that say what to do instead.
func TestAnOutgoingWebhookAddressIsCheckedWhenItIsSaved(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "")
	t.Setenv("METIS_HTTP_ALLOWED_HOSTS", "")
	cases := []struct {
		name, url string
		accepted  bool
	}{
		{"a public https address", "https://ledger.example.com/hooks/metis", true},
		{"surrounding spaces are trimmed", "  https://ledger.example.com/hooks  ", true},
		{"nothing", "", false},
		{"not an address", "ledger", false},
		{"plain http", "http://ledger.example.com/hooks", false},
		{"another scheme", "ftp://ledger.example.com/hooks", false},
		{"a password in the address", "https://user:secret@ledger.example.com/hooks", false},
		{"a fragment", "https://ledger.example.com/hooks#part", false},
		{"loopback", "https://127.0.0.1/hooks", false},
		{"localhost by name", "https://localhost/hooks", false},
		{"a private network", "https://10.0.4.12/hooks", false},
		{"the cloud metadata endpoint", "https://169.254.169.254/latest/meta-data", false},
		{"too long", "https://ledger.example.com/" + strings.Repeat("a", maxOutgoingWebhookURLLength), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := OutgoingWebhookURL(tc.url)
			if tc.accepted && err != nil {
				t.Fatalf("refused %q: %v", tc.url, err)
			}
			if !tc.accepted {
				if err == nil {
					t.Fatalf("accepted %q", tc.url)
				}
				if !errors.Is(err, apierr.ErrInvalidArgument) {
					t.Errorf("refusal of %q is not a validation error: %v", tc.url, err)
				}
			}
		})
	}
}

// Plain HTTP is a developer's machine: allowed only where private networks
// already are.
func TestPlainHTTPIsAllowedOnlyWherePrivateNetworksAre(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	if _, err := OutgoingWebhookURL("http://localhost:8080/hooks"); err != nil {
		t.Fatalf("refused a local http address where private networks are allowed: %v", err)
	}
}

func TestTheEventsAskedForAreKnownAndListedOnce(t *testing.T) {
	if _, err := outgoingWebhookEvents(nil); err == nil {
		t.Error("a webhook with no events was accepted; it would never be sent anything")
	}
	if _, err := outgoingWebhookEvents([]entities.OutgoingWebhookEvent{"process.exploded"}); err == nil {
		t.Error("an event nothing raises was accepted")
	}
	got, err := outgoingWebhookEvents([]entities.OutgoingWebhookEvent{
		entities.OutgoingTaskCompleted, entities.OutgoingProcessStarted, entities.OutgoingTaskCompleted,
	})
	if err != nil {
		t.Fatalf("refused known events: %v", err)
	}
	want := []entities.OutgoingWebhookEvent{entities.OutgoingProcessStarted, entities.OutgoingTaskCompleted}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("events = %v, want %v: each once, in the order a form lists them", got, want)
	}
}

// What an attempt leaves a delivery as: the whole retry policy.
func TestAnAttemptSettlesADeliveryAsTheScheduleSays(t *testing.T) {
	at := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	owed := func(attempts int) entities.OutgoingWebhookDelivery {
		return entities.OutgoingWebhookDelivery{ID: uuid.New(), Status: entities.DeliveryPending, Attempts: attempts}
	}

	t.Run("a 2xx is delivered", func(t *testing.T) {
		got := settle(owed(1), entities.OutgoingWebhookAttempt{StatusCode: http.StatusNoContent, At: at})
		if got.Status != entities.DeliveryDelivered {
			t.Errorf("status = %s, want delivered", got.Status)
		}
	})
	t.Run("a refusal is tried again after the first wait", func(t *testing.T) {
		got := settle(owed(1), entities.OutgoingWebhookAttempt{StatusCode: http.StatusServiceUnavailable, Error: "503", At: at})
		if got.Status != entities.DeliveryPending {
			t.Fatalf("status = %s, want pending", got.Status)
		}
		wait := got.NextAttemptAt.Sub(at)
		low, high := jitterBounds(outgoingRetrySchedule[0])
		if wait < low || wait > high {
			t.Errorf("next attempt in %s, want between %s and %s", wait, low, high)
		}
	})
	t.Run("the last attempt's failure is final", func(t *testing.T) {
		got := settle(owed(maxOutgoingAttempts()), entities.OutgoingWebhookAttempt{StatusCode: http.StatusBadGateway, Error: "502", At: at})
		if got.Status != entities.DeliveryFailed {
			t.Errorf("status = %s, want failed after %d attempts", got.Status, maxOutgoingAttempts())
		}
	})
	t.Run("an address the installation refuses is not retried", func(t *testing.T) {
		got := settle(owed(1), entities.OutgoingWebhookAttempt{Error: "blocked", Permanent: true, At: at})
		if got.Status != entities.DeliveryFailed {
			t.Errorf("status = %s, want failed at once: no retry changes the policy", got.Status)
		}
	})
}

func TestTheRetryScheduleCoversAboutADay(t *testing.T) {
	var total time.Duration
	for _, wait := range outgoingRetrySchedule {
		total += wait
	}
	if total < 20*time.Hour || total > 26*time.Hour {
		t.Errorf("a delivery is retried for %s, which is not about a day", total)
	}
}

// A receiver verifies what it is sent with the scheme the inbound side
// verifies partners with, so the test checks the request with that verifier
// rather than recomputing the signature itself.
func TestADeliveryIsSignedTheWayTheInboundSideVerifies(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	received := make(chan *http.Request, 1)
	bodies := make(chan []byte, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- r
		bodies <- body
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(receiver.Close)

	delivery := deliveryTo(receiver.URL, "a-shared-secret")
	now := time.Now()
	attempt := newOutgoingWebhookSender(5*time.Second).post(t.Context(), delivery, now)
	if !attempt.Accepted() {
		t.Fatalf("attempt not accepted: status %d, error %q", attempt.StatusCode, attempt.Error)
	}
	if attempt.Response != `{"ok":true}` {
		t.Errorf("response = %q, want the receiver's answer kept", attempt.Response)
	}

	r, body := <-received, <-bodies
	if got := r.Header.Get(OutgoingDeliveryIDHeader); got != delivery.ID.String() {
		t.Errorf("delivery id header = %q, want %s", got, delivery.ID)
	}
	if got := r.Header.Get(OutgoingEventHeader); got != string(delivery.Event) {
		t.Errorf("event header = %q, want %s", got, delivery.Event)
	}
	err := webhooksig.VerifyV2(body, "a-shared-secret",
		r.Header.Get(webhooksig.TimestampHeader), r.Header.Get(OutgoingDeliveryIDHeader),
		r.Header.Get(webhooksig.SignatureHeader), now)
	if err != nil {
		t.Errorf("the inbound verifier refused the delivery: %v", err)
	}
	if err := webhooksig.VerifyV2(body, "another-secret",
		r.Header.Get(webhooksig.TimestampHeader), r.Header.Get(OutgoingDeliveryIDHeader),
		r.Header.Get(webhooksig.SignatureHeader), now); err == nil {
		t.Error("the delivery verified under a secret it was not signed with")
	}
}

func TestAReceiversRefusalIsRecordedInItsOwnWords(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(strings.Repeat("maintenance window, try later. ", 100)))
	}))
	t.Cleanup(receiver.Close)

	attempt := newOutgoingWebhookSender(5*time.Second).post(t.Context(), deliveryTo(receiver.URL, "s"), time.Now())
	if attempt.Accepted() || attempt.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("attempt = %+v, want a refused 503", attempt)
	}
	if !strings.Contains(attempt.Error, "503") {
		t.Errorf("error = %q, want it to say what the receiver answered", attempt.Error)
	}
	if len(attempt.Response) > maxResponseExcerpt || !strings.HasPrefix(attempt.Response, "maintenance window") {
		t.Errorf("response excerpt is %d bytes starting %.30q, want the start of the answer and at most %d bytes",
			len(attempt.Response), attempt.Response, maxResponseExcerpt)
	}
}

// A 3xx would move the delivery and its signature to an address nobody
// named, so it is the receiver's answer and not an instruction.
func TestARedirectIsNotFollowed(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	elsewhere := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the redirect was followed")
	}))
	t.Cleanup(elsewhere.Close)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(receiver.Close)

	attempt := newOutgoingWebhookSender(5*time.Second).post(t.Context(), deliveryTo(receiver.URL, "s"), time.Now())
	if attempt.Accepted() || attempt.StatusCode != http.StatusTemporaryRedirect {
		t.Errorf("attempt = %+v, want the 307 recorded as a refusal", attempt)
	}
}

func TestAReceiverThatDoesNotAnswerInTimeIsRetried(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	release := make(chan struct{})
	receiver := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); receiver.Close() })

	attempt := newOutgoingWebhookSender(200*time.Millisecond).post(t.Context(), deliveryTo(receiver.URL, "s"), time.Now())
	if attempt.Accepted() || attempt.Permanent {
		t.Fatalf("attempt = %+v, want a failure worth retrying", attempt)
	}
	if !strings.Contains(attempt.Error, "in time") {
		t.Errorf("error = %q, want it to say the receiver did not answer in time", attempt.Error)
	}
}

// The dialler checks the resolved address on every attempt, whatever was
// checked when the address was saved: a name can be pointed somewhere else
// afterwards.
func TestAnAddressInsideThePrivateNetworkIsRefusedAtTheDial(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "")
	t.Setenv("METIS_HTTP_ALLOWED_HOSTS", "")
	receiver := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("a loopback receiver was reached")
	}))
	t.Cleanup(receiver.Close)

	attempt := newOutgoingWebhookSender(5*time.Second).post(t.Context(), deliveryTo(receiver.URL, "s"), time.Now())
	if attempt.Accepted() || !attempt.Permanent {
		t.Errorf("attempt = %+v, want a permanent refusal", attempt)
	}
}

func TestShutdownIsNotAPermanentFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	text, permanent := describeSendFailure(ctx.Err())
	if permanent || !strings.Contains(text, "shutting down") {
		t.Errorf("a cancelled attempt = (%q, %v), want a retry that says the server stopped", text, permanent)
	}
}

func deliveryTo(target, secret string) entities.OutgoingWebhookDelivery {
	return entities.OutgoingWebhookDelivery{
		ID:      uuid.Must(uuid.NewV7()),
		Webhook: &entities.OutgoingWebhook{ID: uuid.New(), URL: target, Secret: secret},
		Event:   entities.OutgoingTaskCompleted,
		Body:    []byte(`{"event":"task.completed"}`),
		Status:  entities.DeliveryPending,
	}
}

func jitterBounds(wait time.Duration) (time.Duration, time.Duration) {
	spread := time.Duration(float64(wait) * retryJitter)
	return wait - spread, wait + spread
}
