package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// IntegrationAPIKeyRepository keeps the keys organizations give other
// systems, in the main database beside the organizations they belong to.
type IntegrationAPIKeyRepository interface {
	// Create records a key under the hash of its token.
	Create(ctx context.Context, key entities.IntegrationAPIKey, tokenHash string) error
	// ListByOrganization returns an organization's keys, newest first, revoked
	// ones included so its administrators can see what stopped working and
	// when.
	ListByOrganization(ctx context.Context, organizationID uuid.UUID) ([]entities.IntegrationAPIKey, error)
	// Revoke stops one of the organization's keys. apierr.ErrNotFound when it
	// has no unrevoked key with that id.
	Revoke(ctx context.Context, organizationID, id uuid.UUID, at time.Time) error
	// GetUnrevokedByHash returns the unrevoked key a token is, expired or not.
	// apierr.ErrNotFound when there is none.
	GetUnrevokedByHash(ctx context.Context, tokenHash string) (entities.IntegrationAPIKey, error)
	// MarkUsed records when a key was last used.
	MarkUsed(ctx context.Context, id uuid.UUID, at time.Time) error
}
