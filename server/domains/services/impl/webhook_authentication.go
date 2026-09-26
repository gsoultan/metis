package impl

import (
	"errors"
	"fmt"
	"time"

	"github.com/gsoultan/metis/internal/pkg/webhooksig"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/rs/zerolog/log"
)

// ErrLegacySignatureRefused is returned for a delivery signed the legacy way to
// a webhook that does not, or no longer does, accept that.
//
// It is returned only once the legacy signature has matched, so it only ever
// reaches somebody holding the secret — which is what lets the reply say
// plainly what to do instead. Anyone else gets the refusal a forged signature
// gets.
var ErrLegacySignatureRefused = errors.New("webhook: this webhook does not accept legacy signatures, which cover the body alone and let a captured delivery be replayed")

// howToSignV2 finishes that reply: enough to move a sender to v2 without
// sending anybody to look anything up.
//
// In words rather than as <timestamp>.<delivery id>.<raw body>: the reply is
// JSON, whose encoder escapes angle brackets, and <timestamp> is not
// something to hand a person as instructions.
const howToSignV2 = "Sign with v2, using the same secret: send " + webhooksig.TimestampHeader +
	" (the current Unix time in seconds), X-Delivery-Id (your ID for this event, the same on every retry) and " +
	webhooksig.SignatureHeader + ": v2= followed by the hex HMAC-SHA256 of the timestamp, the delivery ID and the raw body, joined with dots"

// authenticateWebhookDelivery checks a delivery against the clock, and puts in
// the log what the reply does not: which webhook refused what, and which
// senders are still signing the legacy way while there is time to move them.
//
// The legacy deadline is returned when the delivery was signed that way, so the
// reply can carry it too.
func authenticateWebhookDelivery(hook models.WebhookModel, delivery entities.WebhookDelivery) (*time.Time, error) {
	legacyUntil, err := authenticateDelivery(hook, delivery, time.Now())
	if err != nil {
		log.Warn().
			Err(err).
			Str("webhook", hook.Name).
			Str("token", redactToken(delivery.Token)).
			Msg("Refused a webhook delivery that did not authenticate")
		return nil, err
	}
	if legacyUntil != nil {
		log.Warn().
			Str("webhook", hook.Name).
			Time("legacy_signatures_until", *legacyUntil).
			Msg("Accepted a webhook delivery signed the legacy way; its sender must move to v2 before this webhook stops accepting that")
	}
	return legacyUntil, nil
}

// authenticateDelivery decides whether a delivery came from whoever holds the
// webhook's secret, returning the legacy deadline when it was signed the legacy
// way.
//
// A delivery that carries a v2 signature is judged by v2 alone. It is never
// tried against the legacy scheme as well: a sender that has moved to v2 gets
// v2's protection — a signed timestamp and a signed delivery ID — and nothing
// that arrives looking like theirs can fall back to the scheme that has none.
//
// A legacy signature is accepted only while the webhook's window for it is
// open. A webhook with no window never accepts one; absent a deadline, the
// answer is no.
func authenticateDelivery(hook models.WebhookModel, delivery entities.WebhookDelivery, now time.Time) (*time.Time, error) {
	if delivery.Signature != "" {
		return nil, webhooksig.VerifyV2(delivery.Body, hook.Secret, delivery.Timestamp, delivery.DeliveryID, delivery.Signature, now)
	}
	if err := webhooksig.Verify(delivery.Body, hook.Secret, delivery.LegacySignature); err != nil {
		return nil, err
	}

	until := hook.LegacySignaturesUntil
	if until == nil {
		return nil, fmt.Errorf("%w; it was created to accept v2 only. %s", ErrLegacySignatureRefused, howToSignV2)
	}
	if !now.Before(*until) {
		return nil, fmt.Errorf("%w; it stopped accepting them at %s. %s",
			ErrLegacySignatureRefused, until.UTC().Format(time.RFC3339), howToSignV2)
	}
	return until, nil
}
