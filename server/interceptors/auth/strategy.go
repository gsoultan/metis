package auth

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// SecurityStrategy defines the contract for different authentication strategies.
type SecurityStrategy interface {
	Authenticate(ctx context.Context, token string) (any, error)
}

// JWTStrategy implements authentication using JWT tokens.
type JWTStrategy struct {
	validateToken func(ctx context.Context, token string) (entities.User, error)
}

func NewJWTStrategy(validateToken func(ctx context.Context, token string) (entities.User, error)) *JWTStrategy {
	return &JWTStrategy{validateToken: validateToken}
}

func (s *JWTStrategy) Authenticate(ctx context.Context, token string) (any, error) {
	return s.validateToken(ctx, token)
}

// OIDCStrategy implements authentication using OIDC providers.
type OIDCStrategy struct {
	validateToken func(ctx context.Context, token string) (any, error)
}

func NewOIDCStrategy(validateToken func(ctx context.Context, token string) (any, error)) *OIDCStrategy {
	return &OIDCStrategy{validateToken: validateToken}
}

func (s *OIDCStrategy) Authenticate(ctx context.Context, token string) (any, error) {
	return s.validateToken(ctx, token)
}
