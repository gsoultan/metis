// Package webhooksig checks that a webhook delivery came from who it claims.
//
// A webhook endpoint is public: it has to be, because a partner's configuration
// screen has nowhere to put a bearer token that this engine would recognise.
// What stands between it and anyone on the internet is a signature — an HMAC of
// the exact bytes delivered, computed with a secret only the two ends know.
//
// The checking is small and the ways to get it wrong are well known, so they are
// all handled here rather than at the call site: comparison in constant time, no
// trust in a header's claimed algorithm, and a decision about the empty secret
// that fails closed.
package webhooksig

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
)

// ErrNoSecret is returned when a webhook has no secret configured.
//
// Unsigned delivery is not offered. An endpoint that accepts anything posted to
// a URL is not a webhook, it is a way for anyone who has ever seen that URL in a
// proxy log to start business processes.
var ErrNoSecret = errors.New("webhooksig: the webhook has no secret, so nothing can be verified")

// ErrBadSignature is returned when the signature does not match the body.
var ErrBadSignature = errors.New("webhooksig: the signature does not match the delivered body")

// Verify checks a delivery's signature against the body it was computed over.
//
// The signature is compared as bytes, in constant time. A comparison that
// returns early on the first differing character leaks how much of a guess was
// right, and that is enough to find the rest one character at a time.
//
// Several senders prefix the value with the algorithm — `sha256=abc123`. The
// prefix is stripped and *ignored*: the algorithm is ours to choose, not the
// caller's to declare, because a caller who can pick the algorithm can pick a
// weak one.
func Verify(body []byte, secret, provided string) error {
	if strings.TrimSpace(secret) == "" {
		return ErrNoSecret
	}

	expected := Sign(body, secret)
	candidate := normalise(provided)

	// hmac.Equal on the decoded bytes rather than the hex text: two encodings of
	// the same digest differ as strings but not as signatures, and comparing the
	// text would reject a sender that writes uppercase hex.
	expectedBytes, err := hex.DecodeString(expected)
	if err != nil {
		return ErrBadSignature
	}
	candidateBytes, err := hex.DecodeString(candidate)
	if err != nil {
		return ErrBadSignature
	}
	if !hmac.Equal(expectedBytes, candidateBytes) {
		return ErrBadSignature
	}
	return nil
}

// Sign returns the signature for a body, as lower-case hex.
//
// Exported because whoever configures the sending end needs to be told exactly
// what to compute, and because a test that recomputes it independently is a test
// of the documentation rather than of the code.
func Sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// normalise strips an algorithm prefix and lower-cases the hex.
func normalise(provided string) string {
	value := strings.TrimSpace(provided)
	if _, after, found := strings.Cut(value, "="); found {
		value = after
	}
	return strings.ToLower(strings.TrimSpace(value))
}
