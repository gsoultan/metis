package impl

import (
	"time"

	"github.com/gsoultan/metis/internal/pkg/webhooksig"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// authenticateDelivery decides whether a delivery came from whoever holds the
// webhook's secret.
//
// A delivery that carries a v2 signature is judged by v2 alone. It is never
// tried against the legacy scheme as well: a sender that has moved to v2 gets
// v2's protection — a signed timestamp and a signed delivery ID — and nothing
// that arrives looking like theirs can fall back to the scheme that has none.
func authenticateDelivery(hook models.WebhookModel, delivery entities.WebhookDelivery, now time.Time) error {
	if delivery.Signature != "" {
		return webhooksig.VerifyV2(delivery.Body, hook.Secret, delivery.Timestamp, delivery.DeliveryID, delivery.Signature, now)
	}
	return webhooksig.Verify(delivery.Body, hook.Secret, delivery.LegacySignature)
}
