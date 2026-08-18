package webhooksig

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

const secret = "shared-with-the-sender-only"

// The signature computed independently of the implementation, so this tests the
// documented scheme rather than agreeing with whatever Sign happens to do.
func expectedSignature(body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestAGenuineDeliveryIsAccepted(t *testing.T) {
	body := []byte(`{"event":"payment.succeeded","id":"evt_1"}`)
	if err := Verify(body, secret, expectedSignature(body)); err != nil {
		t.Fatalf("a correctly signed delivery was rejected: %v", err)
	}
}

// The point of the whole exercise: a public endpoint that only acts on requests
// from someone who knows the secret.
func TestAForgedDeliveryIsRejected(t *testing.T) {
	body := []byte(`{"event":"payment.succeeded","id":"evt_1"}`)

	cases := map[string]string{
		"no signature at all":     "",
		"someone else's secret":   Sign(body, "a guess"),
		"a signature of nothing":  Sign(nil, secret),
		"not hex":                 "not-a-signature",
		"the right length, wrong": strings.Repeat("a", 64),
	}
	for name, provided := range cases {
		t.Run(name, func(t *testing.T) {
			if err := Verify(body, secret, provided); err == nil {
				t.Error("a forged delivery was accepted")
			}
		})
	}
}

// One byte changed in the body is a different delivery. This is what stops a
// captured signature being replayed against a modified payload.
func TestATamperedBodyIsRejected(t *testing.T) {
	body := []byte(`{"amount":100}`)
	signature := expectedSignature(body)

	if err := Verify([]byte(`{"amount":900}`), secret, signature); err == nil {
		t.Error("a body was changed after signing and still accepted")
	}
}

// Senders prefix the value with the algorithm. The prefix is stripped and
// ignored — the algorithm is ours to choose, not the caller's to declare.
func TestAnAlgorithmPrefixIsStrippedAndNotTrusted(t *testing.T) {
	body := []byte(`{"id":"evt_1"}`)
	signature := expectedSignature(body)

	for _, provided := range []string{
		"sha256=" + signature,
		"sha1=" + signature, // a lie about the algorithm changes nothing
		"md5=" + signature,  // including a weak one
		"  sha256=" + signature + "  ",
		strings.ToUpper(signature), // hex case is not part of the signature
	} {
		if err := Verify(body, secret, provided); err != nil {
			t.Errorf("Verify(%q) = %v, want it accepted", provided, err)
		}
	}
}

// Unsigned delivery is not on offer. An endpoint that accepts anything posted to
// a URL is not a webhook, it is a way for anyone who has seen that URL in a
// proxy log to start business processes.
func TestAWebhookWithNoSecretVerifiesNothing(t *testing.T) {
	body := []byte(`{}`)
	for _, empty := range []string{"", "   "} {
		err := Verify(body, empty, Sign(body, ""))
		if !errors.Is(err, ErrNoSecret) {
			t.Errorf("Verify with secret %q = %v, want ErrNoSecret", empty, err)
		}
	}
}

// An empty body is still a delivery, and still has to be signed.
func TestAnEmptyBodyIsStillVerified(t *testing.T) {
	if err := Verify(nil, secret, expectedSignature(nil)); err != nil {
		t.Errorf("a signed empty body was rejected: %v", err)
	}
	if err := Verify(nil, secret, Sign([]byte("something"), secret)); err == nil {
		t.Error("an empty body was accepted with someone else's signature")
	}
}

// Sign is what the sending end is told to compute, so it has to match the
// scheme exactly.
func TestSignMatchesAnIndependentImplementation(t *testing.T) {
	for _, body := range [][]byte{nil, []byte(""), []byte("a"), []byte(`{"nested":{"x":[1,2,3]}}`)} {
		if got, want := Sign(body, secret), expectedSignature(body); got != want {
			t.Errorf("Sign(%q) = %q, want %q", body, got, want)
		}
	}
}
