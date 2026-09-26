package webhooksig

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The v2 scheme.
//
// A v1 signature covers the body and nothing else. The delivery ID that tells a
// retry from a new event travels beside it unsigned, and nothing says when the
// delivery was made — so anyone who captures one signed delivery can post it
// again under a new ID, as often as they like, and every copy is acted on.
//
// v2 signs all three together:
//
//	X-Metis-Timestamp: <Unix seconds>
//	X-Delivery-Id:     <the sender's ID for the event: no dot, at most 191 characters>
//	X-Metis-Signature: v2=hex(HMAC-SHA256(secret, "<timestamp>.<delivery id>.<raw body>"))
//
// and refuses a delivery signed more than Tolerance from this server's clock.
// A copy sent again inside that window carries an ID already seen and is
// answered as the duplicate it is; the same copy under a new ID no longer
// matches its signature; after the window it is stale. Delivery IDs are
// remembered for days, far longer than the window, so there is no gap between
// the two.
//
// The signed string has one reading. The timestamp is an integer and the
// delivery ID may not contain a dot, so the first two dots are the separators,
// whatever the body holds. Were a dot allowed in the ID, the same bytes could be
// read as a longer ID and a shorter body (a body's own dots, in a decimal or an
// address, are where the reading would shift), and the signature would carry
// over to an ID never seen before. Whether that body was then of any use would
// be up to whatever reads it, which is not a property of the signature.

const (
	// SignatureHeader carries a v2 signature: "v2=" and the hex digest.
	SignatureHeader = "X-Metis-Signature"

	// TimestampHeader carries when a v2 delivery was signed, in Unix seconds.
	TimestampHeader = "X-Metis-Timestamp"

	// Tolerance is how far from this server's clock a v2 timestamp may be,
	// either way. Enough for clocks that drift and a request that queues; short
	// enough that a captured delivery is useless almost at once.
	Tolerance = 5 * time.Minute

	// v2Prefix names the scheme in the header value, so that a later scheme can
	// share the header without the two ever being mistaken for each other.
	v2Prefix = "v2="

	// toleranceMinutes is Tolerance as the messages say it.
	toleranceMinutes = int(Tolerance / time.Minute)
)

// ErrBadTimestamp is returned when a v2 delivery's timestamp is not Unix seconds.
var ErrBadTimestamp = errors.New("webhooksig: X-Metis-Timestamp must be the time the delivery was signed, as a whole number of Unix seconds")

// ErrStale is returned when a v2 delivery was signed too far from now.
var ErrStale = fmt.Errorf("webhooksig: the delivery was not signed within %d minutes of this server's clock", toleranceMinutes)

// MaxDeliveryIDLength is the longest delivery ID a v2 delivery may carry: the
// size of the column the IDs already seen are kept in.
const MaxDeliveryIDLength = 191

// ErrBadDeliveryID is returned when a v2 delivery's ID has a dot or is too long.
var ErrBadDeliveryID = fmt.Errorf("webhooksig: X-Delivery-Id must be at most %d characters and contain no dot: "+
	"the signed string is <timestamp>.<delivery id>.<body>, and a dot in the ID would let the same signature read "+
	"as another ID and another body", MaxDeliveryIDLength)

// ErrNoDeliveryID is returned when a v2 delivery carries no ID.
var ErrNoDeliveryID = errors.New("webhooksig: a v2 delivery must carry X-Delivery-Id — the sender's own ID for the event, the same on every retry — and sign it; without one a copy cannot be told from a new event")

// SignV2 returns the value of the v2 signature header for a delivery.
//
// Exported for the same reason as Sign: it is what the sending end is told to
// compute, and a test that recomputes it independently tests the documentation.
func SignV2(body []byte, secret, timestamp, deliveryID string) string {
	return v2Prefix + hex.EncodeToString(digestV2(body, secret, timestamp, deliveryID))
}

// VerifyV2 checks a v2 delivery: the signature first, and only then whether it
// is fresh and carries an ID.
//
// The order is the point. What comes after the signature are refusals that
// explain themselves, and an explanation is only given to somebody who has shown
// they hold the secret. To anyone else a stale delivery looks exactly like a
// forged one, as an unknown address already does — otherwise the explanations
// would say which addresses exist.
func VerifyV2(body []byte, secret, timestamp, deliveryID, provided string, now time.Time) error {
	if strings.TrimSpace(secret) == "" {
		return ErrNoSecret
	}
	hexDigest, ok := strings.CutPrefix(strings.TrimSpace(provided), v2Prefix)
	if !ok {
		return ErrBadSignature
	}
	candidate, err := hex.DecodeString(hexDigest)
	if err != nil {
		return ErrBadSignature
	}
	if !hmac.Equal(digestV2(body, secret, timestamp, deliveryID), candidate) {
		return ErrBadSignature
	}

	signedAt, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrBadTimestamp
	}
	if err := checkFresh(signedAt, now); err != nil {
		return err
	}
	if deliveryID == "" {
		return ErrNoDeliveryID
	}
	if strings.Contains(deliveryID, ".") || len(deliveryID) > MaxDeliveryIDLength {
		return ErrBadDeliveryID
	}
	return nil
}

// digestV2 is the HMAC of "<timestamp>.<delivery id>.<body>".
func digestV2(body []byte, secret, timestamp, deliveryID string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte{'.'})
	mac.Write([]byte(deliveryID))
	mac.Write([]byte{'.'})
	mac.Write(body)
	return mac.Sum(nil)
}

// checkFresh refuses a timestamp more than Tolerance from now, saying which way.
//
// The comparison is made on the bounds rather than on the difference: a
// timestamp near the edge of int64 would overflow a subtraction and come out
// looking fresh.
func checkFresh(signedAt int64, now time.Time) error {
	limit := int64(Tolerance / time.Second)
	current := now.Unix()
	switch {
	case signedAt < current-limit:
		return fmt.Errorf("%w: X-Metis-Timestamp is more than %d minutes behind it. Sign every attempt, retries included, at the moment it is sent, and check the sending machine's clock",
			ErrStale, toleranceMinutes)
	case signedAt > current+limit:
		return fmt.Errorf("%w: X-Metis-Timestamp is more than %d minutes ahead of it. Send Unix time in seconds, not milliseconds, and check the sending machine's clock",
			ErrStale, toleranceMinutes)
	}
	return nil
}
