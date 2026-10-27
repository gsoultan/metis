package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// PersonAPIKeyRepository keeps the keys People give their own systems, in the
// main database beside the directory they belong to.
type PersonAPIKeyRepository interface {
	// Create records a key under the hash of its token.
	Create(ctx context.Context, key entities.PersonAPIKey, tokenHash string) error
	// ListByParticipant returns a person's keys, newest first, revoked ones
	// included so they can see what stopped working and when.
	ListByParticipant(ctx context.Context, participantID uuid.UUID) ([]entities.PersonAPIKey, error)
	// Revoke stops one of the person's keys. apierr.ErrNotFound when they have
	// no live key with that id.
	Revoke(ctx context.Context, participantID, id uuid.UUID, at time.Time) error
	// GetLiveByHash returns the unrevoked key a token is. apierr.ErrNotFound
	// when there is none.
	GetLiveByHash(ctx context.Context, tokenHash string) (entities.PersonAPIKey, error)
	// MarkUsed records when a key was last used.
	MarkUsed(ctx context.Context, id uuid.UUID, at time.Time) error
}
