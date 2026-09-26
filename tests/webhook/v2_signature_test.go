package webhook_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/transports/https/webhooks"
)

// A v1 signature covers the body and nothing else. The delivery ID that tells a
// retry from a new event travels beside it unsigned, and nothing says when the
// delivery was made, so anyone who captured one signed delivery could post it
// again under a new ID as often as they liked — and every copy was acted on.
//
// v2 signs the timestamp and the delivery ID with the body. These tests drive
// the public endpoint with the headers a partner is told to send, and compute
// the signature the way docs/integration.md tells them to rather than asking
// webhooksig: a test that agreed with the implementation would pass whatever
// the implementation did.

func TestAV2SignedDeliveryIsAccepted(t *testing.T) {
	h := newWebhookHarness(t)
	engine := &countingEngine{ExecutionEngine: h.engine}
	endpoint := h.endpoint(engine)
	hook := h.register(t, "order.paid", "order.id")
	body := []byte(`{"order":{"id":"ORD-1"}}`)

	reply := post(t, endpoint, hook, v2Delivery(hook, body, "evt-1", time.Now()))
	if reply.Code != http.StatusAccepted {
		t.Fatalf("a correctly signed v2 delivery was answered %d: %s", reply.Code, reply.Body)
	}
	var outcome entities.WebhookOutcome
	if err := json.Unmarshal(reply.Body.Bytes(), &outcome); err != nil {
		t.Fatalf("read the reply: %v", err)
	}
	if outcome.CorrelationKey != "ORD-1" || outcome.Duplicate {
		t.Errorf("outcome = %+v, want ORD-1 read out of the payload and acted on", outcome)
	}
	if engine.sends != 1 {
		t.Errorf("the delivery was acted on %d times, want once", engine.sends)
	}
}

// Five minutes either way. A delivery signed further from this server's clock
// than that is refused, and the reason is said — once the signature has shown
// the sender holds the secret.
func TestAV2DeliveryOutsideFiveMinutesOfTheServerClockIsRefused(t *testing.T) {
	h := newWebhookHarness(t)
	engine := &countingEngine{ExecutionEngine: h.engine}
	endpoint := h.endpoint(engine)
	hook := h.register(t, "order.paid", "")
	body := []byte(`{"order":{"id":"ORD-1"}}`)

	for name, signedAt := range map[string]time.Time{
		"signed ten minutes ago":           time.Now().Add(-10 * time.Minute),
		"signed ten minutes in the future": time.Now().Add(10 * time.Minute),
	} {
		t.Run(name, func(t *testing.T) {
			reply := post(t, endpoint, hook, v2Delivery(hook, body, "evt-"+name, signedAt))
			if reply.Code != http.StatusBadRequest {
				t.Fatalf("a delivery %s was answered %d, want 400: %s", name, reply.Code, reply.Body)
			}
			if !strings.Contains(reply.Body.String(), "X-Metis-Timestamp") {
				t.Errorf("the refusal does not say which header was wrong: %s", reply.Body)
			}
		})
	}

	t.Run("a timestamp in milliseconds", func(t *testing.T) {
		reply := post(t, endpoint, hook, stampedWith(hook, body, "evt-ms", strconv.FormatInt(time.Now().UnixMilli(), 10)))
		if reply.Code != http.StatusBadRequest {
			t.Fatalf("a delivery stamped in milliseconds was answered %d, want 400: %s", reply.Code, reply.Body)
		}
		if !strings.Contains(reply.Body.String(), "seconds") {
			t.Errorf("the refusal does not suggest seconds: %s", reply.Body)
		}
	})

	t.Run("no timestamp at all", func(t *testing.T) {
		reply := post(t, endpoint, hook, stampedWith(hook, body, "evt-unstamped", ""))
		if reply.Code != http.StatusBadRequest {
			t.Fatalf("a delivery with no timestamp was answered %d, want 400: %s", reply.Code, reply.Body)
		}
		if !strings.Contains(reply.Body.String(), "X-Metis-Timestamp") {
			t.Errorf("the refusal does not name the header to send: %s", reply.Body)
		}
	})

	if engine.sends != 0 {
		t.Fatalf("a stale delivery was acted on %d times", engine.sends)
	}

	// Inside the window, either way: a sender's clock a little off is normal.
	for _, skew := range []time.Duration{-4 * time.Minute, 4 * time.Minute} {
		reply := post(t, endpoint, hook, v2Delivery(hook, body, "evt-skew-"+skew.String(), time.Now().Add(skew)))
		if reply.Code != http.StatusAccepted {
			t.Errorf("a delivery signed %s from now was answered %d, want 202: %s", skew, reply.Code, reply.Body)
		}
	}
}

// The delivery ID is what makes a copy recognisable, so it is signed: a
// signature computed for one ID does not authenticate another.
func TestAV2SignatureOnlyAuthenticatesTheDeliveryIDItWasComputedFor(t *testing.T) {
	h := newWebhookHarness(t)
	engine := &countingEngine{ExecutionEngine: h.engine}
	endpoint := h.endpoint(engine)
	hook := h.register(t, "order.paid", "")
	body := []byte(`{"order":{"id":"ORD-1"}}`)

	signedForOne := v2Delivery(hook, body, "evt-1", time.Now())
	sentAsAnother := signedForOne
	sentAsAnother.deliveryID = "evt-2"

	reply := post(t, endpoint, hook, sentAsAnother)
	if reply.Code != http.StatusUnauthorized {
		t.Fatalf("a signature computed for evt-1 was accepted for evt-2: %d %s", reply.Code, reply.Body)
	}
	assertAnonymousRefusal(t, reply)

	if reply := post(t, endpoint, hook, signedForOne); reply.Code != http.StatusAccepted {
		t.Fatalf("the same delivery under the ID it was signed for was answered %d: %s", reply.Code, reply.Body)
	}
	if engine.sends != 1 {
		t.Errorf("acted on %d times, want once — for the ID the signature covers", engine.sends)
	}
}

// The attack v2 exists to stop, end to end.
func TestACapturedV2DeliveryCannotBeReplayed(t *testing.T) {
	h := newWebhookHarness(t)
	engine := &countingEngine{ExecutionEngine: h.engine}
	endpoint := h.endpoint(engine)
	hook := h.register(t, "order.paid", "")
	captured := v2Delivery(hook, []byte(`{"order":{"id":"ORD-1"}}`), "evt-1", time.Now())

	if reply := post(t, endpoint, hook, captured); reply.Code != http.StatusAccepted {
		t.Fatalf("the original delivery was answered %d: %s", reply.Code, reply.Body)
	}

	// The same copy again, inside the five minutes: an ID already seen.
	reply := post(t, endpoint, hook, captured)
	if reply.Code != http.StatusAccepted || !strings.Contains(reply.Body.String(), `"duplicate":true`) {
		t.Fatalf("a copy of a delivery already acted on was answered %d %s, want 202 and a duplicate", reply.Code, reply.Body)
	}

	// The same copy under a fresh ID: the signature no longer matches.
	for _, id := range []string{"evt-2", "evt-3", "EVT-1"} {
		replayed := captured
		replayed.deliveryID = id
		if reply := post(t, endpoint, hook, replayed); reply.Code == http.StatusAccepted {
			t.Errorf("the captured delivery replayed as %q was accepted", id)
		}
	}

	if engine.sends != 1 {
		t.Fatalf("one delivery was acted on %d times", engine.sends)
	}
}

// The signed string is "<timestamp>.<delivery id>.<body>", and a body can hold
// a dot. Moving the boundary — the front of the body into the ID — keeps the
// signed string, and with it the signature. It must still not make a new
// delivery: what is left of the body is never a JSON object, so nothing is
// acted on.
func TestMovingADotDoesNotMakeANewDelivery(t *testing.T) {
	h := newWebhookHarness(t)
	engine := &countingEngine{ExecutionEngine: h.engine}
	endpoint := h.endpoint(engine)
	hook := h.register(t, "order.paid", "")
	captured := v2Delivery(hook, []byte(`{"amount":1.5}`), "evt-1", time.Now())

	moved := captured
	moved.deliveryID = `evt-1.{"amount":1`
	moved.body = []byte(`5}`)
	if partnerSignV2(hook.Secret, moved.timestamp, moved.deliveryID, moved.body) != captured.signature {
		t.Fatal("the rearrangement does not keep the signed string; the test proves nothing")
	}

	if reply := post(t, endpoint, hook, moved); reply.Code == http.StatusAccepted {
		t.Fatalf("a delivery with its dot moved was accepted: %s", reply.Body)
	}
	if engine.sends != 0 {
		t.Fatalf("a delivery with its dot moved was acted on %d times", engine.sends)
	}
}

// Without an ID a copy cannot be told from a new event, so a v2 delivery has to
// carry one — and sign it.
func TestAV2DeliveryMustCarryADeliveryID(t *testing.T) {
	h := newWebhookHarness(t)
	engine := &countingEngine{ExecutionEngine: h.engine}
	endpoint := h.endpoint(engine)
	hook := h.register(t, "order.paid", "")

	reply := post(t, endpoint, hook, v2Delivery(hook, []byte(`{}`), "", time.Now()))
	if reply.Code != http.StatusBadRequest {
		t.Fatalf("a v2 delivery with no ID was answered %d, want 400: %s", reply.Code, reply.Body)
	}
	if !strings.Contains(reply.Body.String(), "X-Delivery-Id") {
		t.Errorf("the refusal does not name the header to send: %s", reply.Body)
	}
	if engine.sends != 0 {
		t.Errorf("a delivery with no ID was acted on %d times", engine.sends)
	}
}

// Everything v2 explains — stale, missing an ID — is explained only after the
// signature has matched. A forged delivery gets what an unknown address gets,
// so the explanations cannot be used to find out which addresses exist.
func TestAForgedV2DeliveryLearnsNothing(t *testing.T) {
	h := newWebhookHarness(t)
	endpoint := h.endpoint(h.engine)
	hook := h.register(t, "order.paid", "")
	body := []byte(`{}`)

	for name, d := range map[string]partnerDelivery{
		"wrong secret, stale":          signedWith("a guess", body, "evt-1", unix(time.Now().Add(-time.Hour))),
		"wrong secret, no id":          signedWith("a guess", body, "", unix(time.Now())),
		"wrong secret, no timestamp":   signedWith("a guess", body, "evt-2", ""),
		"not a v2 value":               {body: body, deliveryID: "evt-3", timestamp: unix(time.Now()), signature: "sha256=" + strings.Repeat("a", 64)},
		"right value, no v2= in front": stripPrefix(v2Delivery(hook, body, "evt-4", time.Now())),
	} {
		t.Run(name, func(t *testing.T) {
			reply := post(t, endpoint, hook, d)
			if reply.Code != http.StatusUnauthorized {
				t.Fatalf("answered %d, want 401: %s", reply.Code, reply.Body)
			}
			assertAnonymousRefusal(t, reply)
		})
	}
}

// harness

// countingEngine records how many times a delivery was acted on.
type countingEngine struct {
	servicecontracts.ExecutionEngine
	sends int
}

func (e *countingEngine) SendMessage(ctx context.Context, projectID uuid.UUID, messageName, correlationKey string, variables map[string]any) error {
	e.sends++
	return e.ExecutionEngine.SendMessage(ctx, projectID, messageName, correlationKey, variables)
}

// endpoint is the public delivery route, served the way production serves it:
// straight off the mux, with no tenant and no authentication in the context.
func (h *webhookHarness) endpoint(engine servicecontracts.EngineEventBus) http.Handler {
	mux := http.NewServeMux()
	webhooks.RegisterRoutes(mux, serviceimpl.NewWebhookService(h.repo, engine))
	return mux
}

// partnerDelivery is what a partner puts on the wire.
type partnerDelivery struct {
	body       []byte
	deliveryID string
	timestamp  string
	// signature is the X-Metis-Signature value.
	signature string
	// legacySignature is a v1 signature, sent as X-Signature-256.
	legacySignature string
}

func post(t *testing.T, endpoint http.Handler, hook entities.Webhook, d partnerDelivery) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/hooks/"+hook.Token, bytes.NewReader(d.body))
	for name, value := range map[string]string{
		"X-Metis-Timestamp": d.timestamp,
		"X-Metis-Signature": d.signature,
		"X-Signature-256":   d.legacySignature,
		"X-Delivery-Id":     d.deliveryID,
	} {
		if value != "" {
			req.Header.Set(name, value)
		}
	}
	reply := httptest.NewRecorder()
	endpoint.ServeHTTP(reply, req)
	return reply
}

// v2Delivery signs a delivery the way docs/integration.md says to.
func v2Delivery(hook entities.Webhook, body []byte, deliveryID string, signedAt time.Time) partnerDelivery {
	return signedWith(hook.Secret, body, deliveryID, unix(signedAt))
}

// stampedWith signs a delivery correctly over a timestamp that is not.
func stampedWith(hook entities.Webhook, body []byte, deliveryID, timestamp string) partnerDelivery {
	return signedWith(hook.Secret, body, deliveryID, timestamp)
}

func signedWith(secret string, body []byte, deliveryID, timestamp string) partnerDelivery {
	return partnerDelivery{
		body:       body,
		deliveryID: deliveryID,
		timestamp:  timestamp,
		signature:  partnerSignV2(secret, timestamp, deliveryID, body),
	}
}

// partnerSignV2 is the documented scheme, written out independently of
// webhooksig: v2= and the hex HMAC-SHA256 of "<timestamp>.<delivery id>.<body>".
func partnerSignV2(secret, timestamp, deliveryID string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "." + deliveryID + "."))
	mac.Write(body)
	return "v2=" + hex.EncodeToString(mac.Sum(nil))
}

func stripPrefix(d partnerDelivery) partnerDelivery {
	d.signature = strings.TrimPrefix(d.signature, "v2=")
	return d
}

func unix(at time.Time) string { return strconv.FormatInt(at.Unix(), 10) }

// assertAnonymousRefusal checks a refusal says nothing a stranger could use.
func assertAnonymousRefusal(t *testing.T, reply *httptest.ResponseRecorder) {
	t.Helper()
	if body := strings.TrimSpace(reply.Body.String()); body != `{"error":"this delivery was not accepted"}` {
		t.Errorf("a refusal to someone who has not shown the secret said more than no: %s", body)
	}
}
