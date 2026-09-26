package auth

import (
	"context"
	"fmt"

	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
)

// errUnrecognisedToken refuses a bearer token that says it is neither kind.
var errUnrecognisedToken = fmt.Errorf(
	"%w: the token is neither one this server signed nor an ID token from the identity provider",
	pkgauth.ErrUnauthorized)

// TokenKindStrategy accepts both kinds of bearer token while OIDC is on: a
// local account's, which /api/v1/login mints, and an ID token from the
// identity provider. Each is checked by its own rules alone.
//
// Which rules is decided by what the token says it is (see kindOf), not by
// trying one set and falling back to the other. A fallback makes the weaker
// check the one that counts: a token claiming to be the provider's, refused by
// the provider's rules, would be tried again as a local token — and the local
// rules do not read an issuer, so they would take one signed with JWT_SECRET
// whatever provider it named. It would also turn every refusal into the second
// check's reason, hiding which rules refused the token. Dispatching on the
// token's own statement shows each token to one set of rules, once, and a
// refusal is final.
type TokenKindStrategy struct {
	local            SecurityStrategy
	identityProvider SecurityStrategy
}

// NewTokenKindStrategy authenticates local tokens with local and ID tokens with
// identityProvider.
func NewTokenKindStrategy(local, identityProvider SecurityStrategy) *TokenKindStrategy {
	return &TokenKindStrategy{local: local, identityProvider: identityProvider}
}

// Authenticate returns the account a token signs in as, from the rules of the
// kind it says it is, or their refusal as it is — a 403 for somebody the
// provider vouches for and nothing places stays a 403.
func (s *TokenKindStrategy) Authenticate(ctx context.Context, token string) (any, error) {
	switch kindOf(token) {
	case tokenLocal:
		return s.local.Authenticate(ctx, token)
	case tokenIdentityProvider:
		return s.identityProvider.Authenticate(ctx, token)
	default:
		return nil, errUnrecognisedToken
	}
}
