package auth

import (
	"context"

	"github.com/gsoultan/metis/internal/pkg/integrationkey"
	"github.com/gsoultan/metis/server/domains/entities"
)

// IntegrationKeyStrategy takes an organization's integration API key, and
// hands every other bearer token to the strategy it wraps.
//
// Which rules check a token is decided by its shape, as TokenKindStrategy
// decides between local and identity-provider tokens: a key starts "mik_",
// which no JWT can, so a key is never shown to the sign-in rules and a
// sign-in token never to the key's. A refusal from either is final.
type IntegrationKeyStrategy struct {
	keys func(ctx context.Context, token string) (entities.User, error)
	next SecurityStrategy
}

// NewIntegrationKeyStrategy authenticates integration API keys with keys and
// anything else with next.
func NewIntegrationKeyStrategy(keys func(ctx context.Context, token string) (entities.User, error), next SecurityStrategy) *IntegrationKeyStrategy {
	return &IntegrationKeyStrategy{keys: keys, next: next}
}

// Authenticate returns the principal a token acts as.
func (s *IntegrationKeyStrategy) Authenticate(ctx context.Context, token string) (any, error) {
	if integrationkey.Is(token) {
		return s.keys(ctx, token)
	}
	return s.next.Authenticate(ctx, token)
}
