// Package totp is the time-based one-time password of RFC 6238, as every
// authenticator app reads one: HMAC-SHA1, six digits, thirty-second steps.
//
// Written here rather than taken from a library because it is forty lines of
// the standard library, and a dependency on the sign-in path is one more thing
// to keep patched.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- RFC 6238 and every authenticator app use HMAC-SHA1; HMAC does not rest on SHA-1's collision resistance.
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	// StepSeconds is how long one code lasts.
	StepSeconds = 30
	// Digits is how long a code is.
	Digits = 6
	// secretBytes is 160 bits, the length RFC 4226 recommends for HMAC-SHA1.
	secretBytes = 20
	// skewSteps is how many steps either side of now a code is still taken
	// from: a phone's clock a little off, or a code typed as it rolled over.
	skewSteps = 1
)

// encoding is how authenticator apps take a secret: base32 with no padding.
var encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewSecret returns a fresh secret, base32 as an authenticator app takes it.
func NewSecret() (string, error) {
	raw := make([]byte, secretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("could not draw a secret: %w", err)
	}
	return encoding.EncodeToString(raw), nil
}

// StepAt is the step a moment falls in.
func StepAt(at time.Time) int64 {
	return at.Unix() / StepSeconds
}

// CodeAt is the code a secret gives in one step.
func CodeAt(secret string, step int64) (string, error) {
	key, err := encoding.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", fmt.Errorf("the secret is not base32: %w", err)
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step)) // #nosec G115 -- a step is a positive count of thirty-second periods since 1970.
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", Digits, value%1_000_000), nil
}

// Match reports the step a code was made in, if it is the code of a step near
// now that is later than after. after is the last step a code was taken from,
// so a code is taken once: a second use of it, or of an earlier one, matches
// nothing.
func Match(secret, code string, now time.Time, after int64) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != Digits {
		return 0, false
	}
	current := StepAt(now)
	for step := current - skewSteps; step <= current+skewSteps; step++ {
		if step <= after {
			continue
		}
		want, err := CodeAt(secret, step)
		if err != nil {
			return 0, false
		}
		if subtle.ConstantTimeCompare([]byte(want), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

// URI is the otpauth:// link an authenticator app reads from a QR code.
func URI(issuer, account, secret string) string {
	label := url.PathEscape(issuer) + ":" + url.PathEscape(account)
	query := url.Values{}
	query.Set("secret", secret)
	query.Set("issuer", issuer)
	query.Set("algorithm", "SHA1")
	query.Set("digits", fmt.Sprint(Digits))
	query.Set("period", fmt.Sprint(StepSeconds))
	return "otpauth://totp/" + label + "?" + query.Encode()
}
