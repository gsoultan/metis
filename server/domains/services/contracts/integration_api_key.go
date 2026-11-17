package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// IntegrationKeyService is the keys an organization gives other systems to
// call the API with: made, listed and revoked by the organization's
// administrators, and presented by those systems in place of a sign-in.
type IntegrationKeyService interface {
	// CreateIntegrationKey makes a key for the organization the request is
	// for, acting there with roles, and returns it with the key itself: the
	// only time the key is ever shown. expiresAt nil never expires.
	CreateIntegrationKey(ctx context.Context, name string, roles []string, expiresAt *time.Time) (entities.IntegrationAPIKey, string, error)
	// ListIntegrationKeys returns the keys of the organization the request is
	// for, never the keys themselves.
	ListIntegrationKeys(ctx context.Context) ([]entities.IntegrationAPIKey, error)
	// RevokeIntegrationKey stops one of the organization's keys at once.
	RevokeIntegrationKey(ctx context.Context, id uuid.UUID) error
	// AuthenticateIntegrationKey returns the principal a key acts as: its
	// organization, with its roles there.
	AuthenticateIntegrationKey(ctx context.Context, token string) (entities.User, error)
}
