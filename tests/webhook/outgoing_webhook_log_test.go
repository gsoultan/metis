package webhook_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/webhooksig"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/tests/testutils"
)

// What an administrator does from a webhook's page: send it a test, read its
// log, and move a delivery on or stop it.

// A test is posted at once, signed as a real delivery is, says exactly what it
// sent, and is logged as a test that the worker never takes up.
func TestASentTestIsSignedLoggedAndNeverRetried(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	var received []byte
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("not today"))
	}))
	t.Cleanup(receiver.Close)

	h := newOutgoingHarness(t)
	hook := h.register(t, receiver.URL+"/hooks", entities.OutgoingTaskCompleted)

	test, err := h.svc.SendOutgoingWebhookTest(h.ctx, hook.ID, "")
	if err != nil {
		t.Fatalf("send test: %v", err)
	}
	if test.Event != entities.OutgoingTaskCompleted {
		t.Errorf("with no event named the test sent %s, want the webhook's first, %s", test.Event, entities.OutgoingTaskCompleted)
	}
	if test.Attempt.StatusCode != http.StatusTeapot || test.Attempt.Response != "not today" || test.Attempt.Accepted() {
		t.Errorf("the test came back %+v, want the receiver's 418 and its words", test.Attempt)
	}
	if string(received) != string(test.Body) {
		t.Errorf("the test says it sent %s, the receiver got %s", test.Body, received)
	}
	err = webhooksig.VerifyV2(received, hook.Secret, test.Headers[webhooksig.TimestampHeader],
		test.Headers[serviceimpl.OutgoingDeliveryIDHeader], test.Headers[webhooksig.SignatureHeader], time.Now())
	if err != nil {
		t.Errorf("the signature the test shows does not verify against what was sent: %v", err)
	}
	var payload entities.OutgoingWebhookPayload
	if err := json.Unmarshal(test.Body, &payload); err != nil {
		t.Fatalf("test body: %v", err)
	}
	if !payload.Test || payload.ProjectID != h.projectID || payload.Task == nil {
		t.Errorf("the test body is %+v, want a task event of the project marked as a test", payload)
	}

	entries, err := h.svc.ListOutgoingWebhookDeliveries(h.ctx, hook.ID, entities.OutgoingWebhookLogFilter{})
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != test.DeliveryID || !entries[0].Test ||
		entries[0].Status != entities.DeliveryFailed || entries[0].LastStatusCode != http.StatusTeapot {
		t.Fatalf("the log is %+v, want the one test, failed with the 418", entries)
	}
	if due := h.claimAll(t, time.Now().Add(48*time.Hour)); len(due) != 0 {
		t.Errorf("the worker took up %d deliveries after a test, want none", len(due))
	}
	if err := h.svc.RetryOutgoingWebhookDelivery(h.ctx, test.DeliveryID); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Errorf("retrying a test = %v, want it refused", err)
	}
}

// A webhook that is switched off can still be tried, which is how a receiver
// is checked before it is switched on.
func TestASwitchedOffWebhookCanStillBeTried(t *testing.T) {
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(receiver.Close)

	h := newOutgoingHarness(t)
	hook := h.register(t, receiver.URL, entities.OutgoingProcessStarted)
	if err := h.svc.SetOutgoingWebhookEnabled(h.ctx, hook.ID, false); err != nil {
		t.Fatalf("switch off: %v", err)
	}
	test, err := h.svc.SendOutgoingWebhookTest(h.ctx, hook.ID, entities.OutgoingProcessCompleted)
	if err != nil {
		t.Fatalf("send test: %v", err)
	}
	if !test.Attempt.Accepted() || test.Event != entities.OutgoingProcessCompleted {
		t.Errorf("the test came back %+v for %s, want a 204 for the event asked for", test.Attempt, test.Event)
	}
	if _, err := h.svc.SendOutgoingWebhookTest(h.ctx, hook.ID, "invoice.paid"); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Errorf("testing an event that does not exist = %v, want it refused", err)
	}
}

// The log reads newest first, a page at a time, and a page ends where the next
// one starts.
func TestTheLogIsReadNewestFirstAPageAtATime(t *testing.T) {
	h := newOutgoingHarness(t)
	hook := h.register(t, "https://ledger.example.com/hooks", entities.OutgoingProcessStarted)
	for range 3 {
		if _, err := h.svc.StartProcess(h.ctx, h.projectID, "purchase", nil); err != nil {
			t.Fatalf("start: %v", err)
		}
	}

	first, err := h.svc.ListOutgoingWebhookDeliveries(h.ctx, hook.ID, entities.OutgoingWebhookLogFilter{Limit: 2})
	if err != nil || len(first) != 2 {
		t.Fatalf("first page: %d entries, err=%v; want 2", len(first), err)
	}
	if first[0].CreatedAt.Before(first[1].CreatedAt) {
		t.Errorf("the log is not newest first: %s then %s", first[0].CreatedAt, first[1].CreatedAt)
	}
	last := first[len(first)-1]
	second, err := h.svc.ListOutgoingWebhookDeliveries(h.ctx, hook.ID, entities.OutgoingWebhookLogFilter{
		Limit:  2,
		Before: &entities.OutgoingWebhookLogCursor{CreatedAt: last.CreatedAt, ID: last.ID},
	})
	if err != nil || len(second) != 1 {
		t.Fatalf("second page: %d entries, err=%v; want the 1 left", len(second), err)
	}
	for _, seen := range first {
		if second[0].ID == seen.ID {
			t.Errorf("delivery %s is on both pages", seen.ID)
		}
	}

	failed, err := h.svc.ListOutgoingWebhookDeliveries(h.ctx, hook.ID, entities.OutgoingWebhookLogFilter{Status: entities.DeliveryFailed})
	if err != nil || len(failed) != 0 {
		t.Errorf("failed deliveries: %d, err=%v; want none", len(failed), err)
	}
	if _, err := h.svc.ListOutgoingWebhookDeliveries(h.ctx, hook.ID, entities.OutgoingWebhookLogFilter{Status: "lost"}); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Errorf("a status that does not exist = %v, want it refused", err)
	}
}

// A pending delivery can be stopped, so nothing more is sent of it, and a
// failed one retried, which makes it due at once.
func TestAPendingDeliveryCanBeStoppedAndAFailedOneRetried(t *testing.T) {
	h := newOutgoingHarness(t)
	hook := h.register(t, "https://ledger.example.com/hooks", entities.OutgoingProcessStarted)
	if _, err := h.svc.StartProcess(h.ctx, h.projectID, "purchase", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	entries, err := h.svc.ListOutgoingWebhookDeliveries(h.ctx, hook.ID, entities.OutgoingWebhookLogFilter{})
	if err != nil || len(entries) != 1 {
		t.Fatalf("log: %d entries, err=%v; want the one owed", len(entries), err)
	}
	id := entries[0].ID

	if err := h.svc.StopOutgoingWebhookDelivery(h.ctx, id); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if due := h.claimAll(t, time.Now()); len(due) != 0 {
		t.Errorf("%d deliveries are due after the only one was stopped", len(due))
	}
	stopped, _ := h.svc.ListOutgoingWebhookDeliveries(h.ctx, hook.ID, entities.OutgoingWebhookLogFilter{})
	if stopped[0].Status != entities.DeliveryFailed || stopped[0].LastError == "" || stopped[0].NextAttemptAt.IsZero() {
		t.Errorf("a stopped delivery reads %+v, want failed and saying why", stopped[0])
	}
	if err := h.svc.StopOutgoingWebhookDelivery(h.ctx, id); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Errorf("stopping it again = %v, want it refused", err)
	}

	if err := h.svc.RetryOutgoingWebhookDelivery(h.ctx, id); err != nil {
		t.Fatalf("retry: %v", err)
	}
	due := h.claimAll(t, time.Now().Add(time.Second))
	if len(due) != 1 || due[0].ID != id {
		t.Fatalf("after a retry %d deliveries are due, want the one retried", len(due))
	}
}

// Another organization's log, tests and deliveries are not there.
func TestAnotherOrganizationsLogIsNotThere(t *testing.T) {
	h := newOutgoingHarness(t)
	hook := h.register(t, "https://ledger.example.com/hooks", entities.OutgoingProcessStarted)
	if _, err := h.svc.StartProcess(h.ctx, h.projectID, "purchase", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	entries, err := h.svc.ListOutgoingWebhookDeliveries(h.ctx, hook.ID, entities.OutgoingWebhookLogFilter{})
	if err != nil || len(entries) != 1 {
		t.Fatalf("log: %d entries, err=%v", len(entries), err)
	}
	delivery := entries[0].ID

	strangerCtx, _, _ := testutils.ScopedProject(t, h.repo)
	for name, act := range map[string]func() error{
		"read the log": func() error {
			_, err := h.svc.ListOutgoingWebhookDeliveries(strangerCtx, hook.ID, entities.OutgoingWebhookLogFilter{})
			return err
		},
		"send a test": func() error {
			_, err := h.svc.SendOutgoingWebhookTest(strangerCtx, hook.ID, "")
			return err
		},
		"retry": func() error { return h.svc.RetryOutgoingWebhookDelivery(strangerCtx, delivery) },
		"stop":  func() error { return h.svc.StopOutgoingWebhookDelivery(strangerCtx, delivery) },
	} {
		if err := act(); !errors.Is(err, apierr.ErrNotFound) {
			t.Errorf("another organization tried to %s: %v, want not found", name, err)
		}
	}
	if after, _ := h.svc.ListOutgoingWebhookDeliveries(h.ctx, hook.ID, entities.OutgoingWebhookLogFilter{}); after[0].Status != entities.DeliveryPending {
		t.Errorf("the delivery is %s after another organization's attempts, want still pending", after[0].Status)
	}
}
