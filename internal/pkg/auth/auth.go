package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/domains/entities"
)

type AuthContextKey string

const (
	UserContextKey AuthContextKey = "user"
)

// EnvOrganizationClaim names the ID-token claim that carries the organizations
// somebody signing in through the identity provider belongs to.
//
// METIS_-prefixed like every setting added since the rename, with OIDC_ after
// it to sit beside OIDC_ISSUER and OIDC_CLIENT_ID, which predate the prefix.
// Unset, nobody who signs in that way can be placed in an organization, and
// each of them is refused.
const EnvOrganizationClaim = "METIS_OIDC_ORGANIZATION_CLAIM"

var (
	ErrUnauthorized         = errors.New("unauthorized: missing or invalid token")
	ErrAuthenticationFailed = errors.New("authentication failed")
)

// idTokenProfile is the part of an ID token that says how somebody is named
// and reached. None of it identifies them; see entities.IdentityClaims.
type idTokenProfile struct {
	Username string `json:"preferred_username"`
	Name     string `json:"name"`
	Email    string `json:"email"`
}

type TokenValidator struct {
	verifier *oidc.IDTokenVerifier
	// organizationClaim is read once, with the provider it belongs to, rather
	// than on every request.
	organizationClaim string
}

func NewTokenValidator(ctx context.Context, issuer string, clientID string) (*TokenValidator, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider: %w", err)
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: clientID})
	return &TokenValidator{
		verifier:          verifier,
		organizationClaim: strings.TrimSpace(envvar.Get(EnvOrganizationClaim)),
	}, nil
}

// OrganizationClaim is the claim organizations are read from, or empty when
// EnvOrganizationClaim does not name one.
func (v *TokenValidator) OrganizationClaim() string { return v.organizationClaim }

// ValidateToken verifies an ID token and returns what it vouches for.
//
// Not who the caller is here: that is the account linked to the token's issuer
// and subject, which signing in resolves. The token's own claims are never the
// principal — in particular a roles claim grants nothing, because roles are
// held on the account and granted by an administrator.
func (v *TokenValidator) ValidateToken(ctx context.Context, tokenString string) (entities.IdentityClaims, error) {
	idToken, err := v.verifier.Verify(ctx, tokenString)
	if err != nil {
		return entities.IdentityClaims{}, fmt.Errorf("failed to verify token: %w", err)
	}

	var profile idTokenProfile
	if err := idToken.Claims(&profile); err != nil {
		return entities.IdentityClaims{}, fmt.Errorf("failed to parse claims: %w", err)
	}
	claims := entities.IdentityClaims{
		Issuer:            idToken.Issuer,
		Subject:           idToken.Subject,
		Username:          profile.Username,
		Name:              profile.Name,
		Email:             profile.Email,
		OrganizationClaim: v.organizationClaim,
	}
	if v.organizationClaim == "" {
		return claims, nil
	}

	var all map[string]json.RawMessage
	if err := idToken.Claims(&all); err != nil {
		return entities.IdentityClaims{}, fmt.Errorf("failed to parse claims: %w", err)
	}
	raw, present := all[v.organizationClaim]
	claims.HasOrganizationClaim = present
	claims.Organizations = claimValues(raw)
	return claims, nil
}

// claimValues reads a claim that is one string or a list of them.
//
// The claim name is matched exactly and at the top level: a provider's custom
// claim is often a URL, full of dots, so a dot is not read as a path. Anything
// that is not a string — a number, an object, null, a list entry that is one of
// those — names nothing, which the sign-in then refuses rather than guesses at.
func claimValues(raw json.RawMessage) []string {
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return nonEmpty([]string{one})
	}
	var many []any
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil
	}
	values := make([]string, 0, len(many))
	for _, value := range many {
		if s, ok := value.(string); ok {
			values = append(values, s)
		}
	}
	return nonEmpty(values)
}

func nonEmpty(values []string) []string {
	kept := values[:0]
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			kept = append(kept, value)
		}
	}
	return kept
}
