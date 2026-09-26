package webhook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A legacy (v1) signature covers the body alone, so a delivery signed that way
// can be captured and posted again under a new delivery ID, as often as anyone
// likes, and each copy is acted on.
//
// Webhooks set up before v2 existed accept it for a window — migration 25
// gives them ninety days — so their senders are not cut off at once. A webhook
// created since accepts v2 only. After its window, a legacy-signed delivery is
// refused with a message saying how to move.

// The attack, before and after: a legacy-signed delivery posted again under new
// IDs used to be acted on every time.
func TestACapturedLegacyDeliveryReplayedUnderNewIDsIsNotActedOn(t *testing.T) {
	h := newWebhookHarness(t)
	engine := &countingEngine{ExecutionEngine: h.engine}
	endpoint := h.endpoint(engine)
	hook := h.register(t, "order.paid", "") // created now, so no legacy window
	captured := legacyDelivery(hook, []byte(`{"order":{"id":"ORD-1"}}`), "evt-1")

	for _, id := range []string{"evt-1", "evt-2", "evt-3"} {
		replayed := captured
		replayed.deliveryID = id
		reply := post(t, endpoint, hook, replayed)
		if reply.Code != http.StatusBadRequest {
			t.Errorf("a legacy-signed delivery sent as %s was answered %d, want 400: %s", id, reply.Code, reply.Body)
			continue
		}
		assertSaysHowToMoveToV2(t, reply)
	}
	if engine.sends != 0 {
		t.Fatalf("one legacy-signed delivery, sent under three IDs, was acted on %d times", engine.sends)
	}
}

// A webhook from before v2 keeps working the legacy way until its window
// closes, and not a moment after.
func TestALegacySignatureIsAcceptedOnlyWhileTheWindowIsOpen(t *testing.T) {
	h := newWebhookHarness(t)
	engine := &countingEngine{ExecutionEngine: h.engine}
	endpoint := h.endpoint(engine)
	hook := h.register(t, "order.paid", "")
	opensUntil := time.Now().Add(time.Hour).Truncate(time.Second)
	h.setLegacyWindow(t, hook, opensUntil)

	captured := legacyDelivery(hook, []byte(`{"order":{"id":"ORD-1"}}`), "evt-1")
	reply := post(t, endpoint, hook, captured)
	if reply.Code != http.StatusAccepted {
		t.Fatalf("a legacy-signed delivery inside the window was answered %d: %s", reply.Code, reply.Body)
	}
	// The reply carries the deadline, so it reaches whoever reads the sender's
	// delivery log.
	if until := legacyDeadlineIn(t, reply); !until.Equal(opensUntil) {
		t.Errorf("the reply says legacy signatures end at %v, want %v", until, opensUntil)
	}

	// Inside the window the legacy scheme is what it always was: a copy under a
	// new ID is acted on. That is the risk the window accepts, and why it ends.
	replayed := captured
	replayed.deliveryID = "evt-2"
	if reply := post(t, endpoint, hook, replayed); reply.Code != http.StatusAccepted {
		t.Fatalf("inside the window, the legacy scheme changed: %d %s", reply.Code, reply.Body)
	}

	closedAt := time.Now().Add(-time.Minute).Truncate(time.Second)
	h.setLegacyWindow(t, hook, closedAt)
	replayed.deliveryID = "evt-3"
	reply = post(t, endpoint, hook, replayed)
	if reply.Code != http.StatusBadRequest {
		t.Fatalf("after the window, a legacy-signed delivery was answered %d, want 400: %s", reply.Code, reply.Body)
	}
	assertSaysHowToMoveToV2(t, reply)
	if !strings.Contains(reply.Body.String(), closedAt.UTC().Format(time.RFC3339)) {
		t.Errorf("the refusal does not say when legacy signatures stopped (%s): %s", closedAt.UTC().Format(time.RFC3339), reply.Body)
	}
	if engine.sends != 2 {
		t.Errorf("acted on %d times, want twice — both inside the window", engine.sends)
	}
}

// v2 works throughout, so a sender can move whenever it is ready.
func TestAV2DeliveryIsAcceptedWhileTheLegacyWindowIsOpen(t *testing.T) {
	h := newWebhookHarness(t)
	endpoint := h.endpoint(h.engine)
	hook := h.register(t, "order.paid", "")
	h.setLegacyWindow(t, hook, time.Now().Add(time.Hour))

	reply := post(t, endpoint, hook, v2Delivery(hook, []byte(`{}`), "evt-1", time.Now()))
	if reply.Code != http.StatusAccepted {
		t.Fatalf("a v2 delivery to a webhook still accepting legacy signatures was answered %d: %s", reply.Code, reply.Body)
	}
	if strings.Contains(reply.Body.String(), "legacy_signatures_until") {
		t.Errorf("a v2 delivery was told about the legacy deadline: %s", reply.Body)
	}
}

// Saying how to move is saying the address exists and the secret was right, so
// it is said only when the legacy signature matched. Anything else gets what
// an unknown address gets.
func TestAForgedLegacyDeliveryIsToldNothing(t *testing.T) {
	h := newWebhookHarness(t)
	endpoint := h.endpoint(h.engine)
	body := []byte(`{}`)

	for name, window := range map[string]time.Duration{
		"a webhook with no window":       0,
		"a webhook whose window shut":    -time.Hour,
		"a webhook whose window is open": time.Hour,
	} {
		t.Run(name, func(t *testing.T) {
			hook := h.register(t, "order.paid", "")
			if window != 0 {
				h.setLegacyWindow(t, hook, time.Now().Add(window))
			}
			forged := partnerDelivery{body: body, deliveryID: "evt-1", legacySignature: "sha256=" + partnerSignV1("a guess", body)}
			reply := post(t, endpoint, hook, forged)
			if reply.Code != http.StatusUnauthorized {
				t.Fatalf("answered %d, want 401: %s", reply.Code, reply.Body)
			}
			assertAnonymousRefusal(t, reply)
		})
	}
}

// The list is what the webhooks screen shows. A webhook created now accepts v2
// only and says nothing about legacy signatures; one inside its window says
// until when.
func TestTheListSaysUntilWhenAWebhookAcceptsLegacySignatures(t *testing.T) {
	h := newWebhookHarness(t)
	created := h.register(t, "order.paid", "")
	if raw := asJSON(t, created); strings.Contains(raw, "legacy_signatures_until") {
		t.Fatalf("a webhook created now accepts legacy signatures: %s", raw)
	}

	until := time.Now().Add(90 * 24 * time.Hour).Truncate(time.Second)
	older := h.register(t, "order.shipped", "")
	h.setLegacyWindow(t, older, until)

	listed, err := h.service.ListWebhooks(h.ctx, h.projectID)
	if err != nil {
		t.Fatalf("list webhooks: %v", err)
	}
	shown := map[string]*time.Time{}
	for _, hook := range listed {
		var wire struct {
			Until *time.Time `json:"legacy_signatures_until"`
		}
		if err := json.Unmarshal([]byte(asJSON(t, hook)), &wire); err != nil {
			t.Fatalf("read the listed webhook back: %v", err)
		}
		shown[hook.MessageName] = wire.Until
	}
	if got := shown["order.paid"]; got != nil {
		t.Errorf("the list gives a v2-only webhook a legacy window, until %v", got)
	}
	if got := shown["order.shipped"]; got == nil || !got.Equal(until) {
		t.Errorf("the list says legacy signatures are accepted until %v, want %v", got, until)
	}
}

// harness

// setLegacyWindow gives a webhook the deadline migration 25 gives one that
// existed before v2.
func (h *webhookHarness) setLegacyWindow(t *testing.T, hook entities.Webhook, until time.Time) {
	t.Helper()
	if err := h.db.WithContext(t.Context()).Exec(
		`UPDATE webhooks SET legacy_signatures_until = ? WHERE id = ?`, until.UTC(), hook.ID).Error; err != nil {
		t.Fatalf("set the legacy window: %v", err)
	}
}

// legacyDelivery signs a delivery the legacy way: the body alone.
func legacyDelivery(hook entities.Webhook, body []byte, deliveryID string) partnerDelivery {
	return partnerDelivery{body: body, deliveryID: deliveryID, legacySignature: "sha256=" + partnerSignV1(hook.Secret, body)}
}

// partnerSignV1 is the legacy scheme, written out independently of webhooksig.
func partnerSignV1(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// assertSaysHowToMoveToV2 checks a refusal tells a sender what to do instead.
func assertSaysHowToMoveToV2(t *testing.T, reply *httptest.ResponseRecorder) {
	t.Helper()
	body := reply.Body.String()
	for _, needed := range []string{"legacy", "v2", "X-Metis-Timestamp", "X-Delivery-Id", "X-Metis-Signature", "HMAC-SHA256"} {
		if !strings.Contains(body, needed) {
			t.Errorf("the refusal does not say %q, so it does not say how to move to v2: %s", needed, body)
		}
	}
}

func legacyDeadlineIn(t *testing.T, reply *httptest.ResponseRecorder) time.Time {
	t.Helper()
	var outcome struct {
		Until time.Time `json:"legacy_signatures_until"`
	}
	if err := json.Unmarshal(reply.Body.Bytes(), &outcome); err != nil {
		t.Fatalf("read the reply: %v", err)
	}
	return outcome.Until
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return string(raw)
}
