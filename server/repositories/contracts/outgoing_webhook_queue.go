package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// OutgoingWebhookQueue is what outgoing webhooks are owed, and the record of
// what came of it.
//
// It is not tenant-scoped. Its writer is the engine, inside a step it is
// already running for a project, and its reader is the delivery worker, which
// spans every tenant by design, as the job worker does.
type OutgoingWebhookQueue interface {
	// Enqueue owes body to every switched-on webhook of the project that asked
	// for event, inside the caller's transaction, and says how many that was.
	Enqueue(ctx context.Context, projectID uuid.UUID, event entities.OutgoingWebhookEvent, body []byte) (int, error)

	// ClaimDue takes up to limit deliveries whose time has come, of webhooks
	// still switched on, and holds each for lease by moving its next attempt
	// that far on. Each comes with its webhook's URL and secret, unsealed, and
	// with Attempts already counting the attempt being made.
	ClaimDue(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]entities.OutgoingWebhookDelivery, error)

	// Settle records what came of the attempt a claim was for. Status and
	// NextAttemptAt are where the delivery stands after it. A delivery claimed
	// again since — its lease ran out while this attempt was still going — is
	// left to the newer claim.
	Settle(ctx context.Context, delivery entities.OutgoingWebhookDelivery, attempt entities.OutgoingWebhookAttempt) error

	// ForgetBefore removes the deliveries created before cutoff, settled or
	// not.
	ForgetBefore(ctx context.Context, cutoff time.Time) (int64, error)
}
