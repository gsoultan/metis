package impl

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/rs/zerolog/log"
)

// CorrelationBackfillResult reports what one backfill run did, so an operator can
// see whether any instance was left stranded rather than having to infer it.
type CorrelationBackfillResult struct {
	Scanned    int
	Rewritten  int
	Unresolved int
}

// BackfillMessageCorrelationKeys re-resolves message subscriptions whose
// correlation key was persisted as a raw ${...} template.
//
// Correlation keys used to be stored verbatim rather than evaluated per
// instance, so every subscription of a definition held the same template text.
// Those rows cannot match a real inbound correlation value, which means any
// instance already parked on a message catch event would wait forever once keys
// started resolving. This walks those rows once at startup and rewrites each key
// from its own instance's variables.
//
// It is idempotent: a rewritten key no longer contains "${", so a later run does
// not select it again.
//
// A subscription that cannot be resolved is counted and logged but left
// untouched. It cannot be repaired automatically, and deleting it would silently
// throw away the instance's only route forward. Startup is not blocked either —
// legacy data that one instance cannot resolve must not stop the server coming
// up. Only an infrastructure failure aborts the run.
func BackfillMessageCorrelationKeys(ctx context.Context, repo repositories.Repository) (CorrelationBackfillResult, error) {
	subs, err := repo.Subscription().ListTemplatedMessageSubscriptions(ctx)
	if err != nil {
		return CorrelationBackfillResult{}, fmt.Errorf("list templated message subscriptions: %w", err)
	}

	result := CorrelationBackfillResult{Scanned: len(subs)}
	for i := range subs {
		sub := &subs[i]
		instance, err := repo.Process().Get(ctx, uuid.UUID(sub.InstanceID))
		if err != nil {
			result.Unresolved++
			log.Warn().Err(err).
				Str("subscriptionId", sub.ID.String()).
				Str("instanceId", sub.InstanceID.String()).
				Msg("Cannot backfill correlation key: the instance could not be loaded")
			continue
		}

		resolved, err := entities.ResolveCorrelationKey(sub.CorrelationKey, instance.Variables)
		if err != nil {
			result.Unresolved++
			log.Warn().Err(err).
				Str("subscriptionId", sub.ID.String()).
				Str("instanceId", sub.InstanceID.String()).
				Str("correlationKey", sub.CorrelationKey).
				Msg("Cannot backfill correlation key: it does not resolve against the instance's variables")
			continue
		}

		if err := repo.Subscription().UpdateCorrelationKey(ctx, uuid.UUID(sub.ID), resolved); err != nil {
			return result, fmt.Errorf("update correlation key for subscription %s: %w", sub.ID, err)
		}
		result.Rewritten++
	}

	if result.Rewritten > 0 || result.Unresolved > 0 {
		log.Info().
			Int("scanned", result.Scanned).
			Int("rewritten", result.Rewritten).
			Int("unresolved", result.Unresolved).
			Msg("Backfilled templated message correlation keys")
	}
	if result.Unresolved > 0 {
		log.Warn().
			Int("unresolved", result.Unresolved).
			Msg("Some message subscriptions still hold an unresolved correlation key; those instances cannot be correlated to and need manual repair")
	}

	return result, nil
}
