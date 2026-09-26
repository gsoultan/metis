package webhooksig

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The published test vector: docs/integration.md gives these four values and
// this signature so a partner can check their implementation before sending
// anything. Computed with Python's hmac and with `openssl dgst -hmac`, not with
// this package.
const (
	vectorSecret     = "your-webhook-secret"
	vectorTimestamp  = "1767225600"
	vectorDeliveryID = "evt_0001"
	vectorBody       = `{"order":{"id":"ORD-1"}}`
	vectorSignature  = "v2=bb84c51b81746277520c49e834388c1fdaa0680755ef30bc087b6a7eba752675"
)

var signedAt = time.Unix(1767225600, 0)

func TestSignV2MatchesThePublishedVector(t *testing.T) {
	if got := SignV2([]byte(vectorBody), vectorSecret, vectorTimestamp, vectorDeliveryID); got != vectorSignature {
		t.Fatalf("SignV2 = %s, want %s", got, vectorSignature)
	}
	if err := VerifyV2([]byte(vectorBody), vectorSecret, vectorTimestamp, vectorDeliveryID, vectorSignature, signedAt); err != nil {
		t.Fatalf("the published vector does not verify: %v", err)
	}
}

// Five minutes either way, inclusive: a sender's clock a little off is normal,
// and a copy kept longer than that is not a delivery any more.
func TestAV2DeliveryIsFreshForFiveMinutesEitherWay(t *testing.T) {
	body := []byte(vectorBody)
	signature := SignV2(body, secret, vectorTimestamp, vectorDeliveryID)

	for _, skew := range []time.Duration{0, Tolerance, -Tolerance, time.Minute, -time.Minute} {
		if err := VerifyV2(body, secret, vectorTimestamp, vectorDeliveryID, signature, signedAt.Add(skew)); err != nil {
			t.Errorf("checked %s after signing: %v, want it accepted", skew, err)
		}
	}
	for _, skew := range []time.Duration{Tolerance + time.Second, -Tolerance - time.Second, 24 * time.Hour, -24 * time.Hour} {
		err := VerifyV2(body, secret, vectorTimestamp, vectorDeliveryID, signature, signedAt.Add(skew))
		if !errors.Is(err, ErrStale) {
			t.Errorf("checked %s after signing: %v, want ErrStale", skew, err)
		}
	}
}

// Which way the clock is off is said, because the fix is different: a
// timestamp behind is an old copy or a slow clock, one far ahead is almost
// always milliseconds.
func TestAStaleDeliverySaysWhichWay(t *testing.T) {
	body := []byte(`{}`)
	for name, tc := range map[string]struct {
		timestamp string
		want      string
	}{
		"behind":       {strconv.FormatInt(signedAt.Add(-time.Hour).Unix(), 10), "behind"},
		"ahead":        {strconv.FormatInt(signedAt.Add(time.Hour).Unix(), 10), "ahead"},
		"milliseconds": {strconv.FormatInt(signedAt.UnixMilli(), 10), "not milliseconds"},
	} {
		t.Run(name, func(t *testing.T) {
			err := VerifyV2(body, secret, tc.timestamp, "evt-1", SignV2(body, secret, tc.timestamp, "evt-1"), signedAt)
			if !errors.Is(err, ErrStale) {
				t.Fatalf("err = %v, want ErrStale", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%q does not say %q", err, tc.want)
			}
		})
	}
}

// Each of the three things signed is covered: change any one after signing and
// the signature no longer matches.
func TestAV2SignatureCoversTheTimestampTheDeliveryIDAndTheBody(t *testing.T) {
	body := []byte(vectorBody)
	signature := SignV2(body, secret, vectorTimestamp, vectorDeliveryID)
	later := strconv.FormatInt(signedAt.Unix()+1, 10)

	for name, tc := range map[string]struct {
		body                  []byte
		timestamp, deliveryID string
	}{
		"another delivery ID":             {body, vectorTimestamp, "evt_0002"},
		"the same ID in another case":     {body, vectorTimestamp, strings.ToUpper(vectorDeliveryID)},
		"another timestamp":               {body, later, vectorDeliveryID},
		"another body":                    {[]byte(`{"order":{"id":"ORD-2"}}`), vectorTimestamp, vectorDeliveryID},
		"the ID moved into the body":      {[]byte(vectorDeliveryID + "." + vectorBody), vectorTimestamp, ""},
		"the timestamp moved into the ID": {body, "", vectorTimestamp + "." + vectorDeliveryID},
	} {
		t.Run(name, func(t *testing.T) {
			err := VerifyV2(tc.body, secret, tc.timestamp, tc.deliveryID, signature, signedAt)
			if !errors.Is(err, ErrBadSignature) {
				t.Errorf("err = %v, want ErrBadSignature", err)
			}
		})
	}
}

// The value names its scheme. A bare digest or another scheme's prefix is not a
// v2 signature, even when the digest itself is right — a v2 header is judged by
// v2 alone, never by whatever the value looks like.
func TestAV2SignatureMustSayV2(t *testing.T) {
	body := []byte(vectorBody)
	digest := strings.TrimPrefix(SignV2(body, secret, vectorTimestamp, vectorDeliveryID), "v2=")

	for _, provided := range []string{"", digest, "sha256=" + digest, "v1=" + digest, "v3=" + digest, "v2=", "v2=not-hex", "v2=" + digest[:62]} {
		err := VerifyV2(body, secret, vectorTimestamp, vectorDeliveryID, provided, signedAt)
		if !errors.Is(err, ErrBadSignature) {
			t.Errorf("VerifyV2(%q) = %v, want ErrBadSignature", provided, err)
		}
	}

	// Hex case is not part of the signature, and surrounding space is not either.
	for _, provided := range []string{"v2=" + strings.ToUpper(digest), "  v2=" + digest + " "} {
		if err := VerifyV2(body, secret, vectorTimestamp, vectorDeliveryID, provided, signedAt); err != nil {
			t.Errorf("VerifyV2(%q) = %v, want it accepted", provided, err)
		}
	}
}

// What a delivery's own values get wrong is explained — but only once the
// signature has matched, so the explanation reaches the secret's holder and
// nobody else. The same mistakes under a signature that does not match are just
// a bad signature.
func TestAV2DeliveryIsExplainedOnlyToWhoeverHoldsTheSecret(t *testing.T) {
	body := []byte(`{}`)
	for name, tc := range map[string]struct {
		timestamp, deliveryID string
		want                  error
	}{
		"no timestamp":         {"", "evt-1", ErrBadTimestamp},
		"a timestamp in words": {"yesterday", "evt-1", ErrBadTimestamp},
		"a fractional second":  {vectorTimestamp + ".5", "evt-1", ErrBadTimestamp},
		"no delivery ID":       {vectorTimestamp, "", ErrNoDeliveryID},
		"stale":                {"1000", "evt-1", ErrStale},
	} {
		t.Run(name, func(t *testing.T) {
			signed := SignV2(body, secret, tc.timestamp, tc.deliveryID)
			if err := VerifyV2(body, secret, tc.timestamp, tc.deliveryID, signed, signedAt); !errors.Is(err, tc.want) {
				t.Errorf("signed with the secret: err = %v, want %v", err, tc.want)
			}
			forged := SignV2(body, "a guess", tc.timestamp, tc.deliveryID)
			if err := VerifyV2(body, secret, tc.timestamp, tc.deliveryID, forged, signedAt); !errors.Is(err, ErrBadSignature) {
				t.Errorf("signed without the secret: err = %v, want ErrBadSignature and nothing more", err)
			}
		})
	}
}

func TestAV2WebhookWithNoSecretVerifiesNothing(t *testing.T) {
	body := []byte(`{}`)
	for _, empty := range []string{"", "   "} {
		signature := SignV2(body, empty, vectorTimestamp, vectorDeliveryID)
		if err := VerifyV2(body, empty, vectorTimestamp, vectorDeliveryID, signature, signedAt); !errors.Is(err, ErrNoSecret) {
			t.Errorf("VerifyV2 with secret %q = %v, want ErrNoSecret", empty, err)
		}
	}
}
