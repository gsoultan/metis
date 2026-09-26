package impl

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/redaction"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/rs/zerolog/log"
)

// SignInThroughIdentityProvider resolves somebody an identity provider vouched
// for to the account they act as here.
//
// From here on the account is the principal, exactly as a local account is: its
// roles are the ones an administrator gave it — a token's own roles claim grants
// nothing — and its organizations are what the tenant resolver places requests
// in. Those organizations are the ones this token's claim names. A request is
// never admitted to an organization because an earlier token's claim named it.
func (s *userService) SignInThroughIdentityProvider(ctx context.Context, claims entities.IdentityClaims) (entities.User, error) {
	placed, account, err := s.signIn(ctx, claims)
	if err != nil {
		return entities.User{}, err
	}
	principal := account.user
	principal.Organizations = organizationShells(placed.organizations)
	return principal, nil
}

// signIn places the claims and reads the account they were placed as.
//
// A placement remembered from a moment ago can name an account an
// administrator has deleted since. It is placed afresh then, once, which gives
// the person a new account rather than refusing somebody the provider still
// vouches for.
func (s *userService) signIn(ctx context.Context, claims entities.IdentityClaims) (identityPlacement, cachedPrincipal, error) {
	key := placementKeyOf(claims)
	placed, err := s.placementFor(ctx, key, claims)
	if err != nil {
		return identityPlacement{}, cachedPrincipal{}, err
	}
	account, err := s.resolvePrincipal(ctx, placed.account)
	if err == nil {
		return placed, account, nil
	}
	s.placements.forget(key)
	if placed, err = s.placementFor(ctx, key, claims); err != nil {
		return identityPlacement{}, cachedPrincipal{}, err
	}
	account, err = s.resolvePrincipal(ctx, placed.account)
	return placed, account, err
}

// placementFor places a sign-in, or recalls how it was placed a moment ago.
func (s *userService) placementFor(ctx context.Context, key placementKey, claims entities.IdentityClaims) (identityPlacement, error) {
	if cached, ok := s.placements.get(key); ok {
		return cached.placement, cached.refusal
	}
	placed, err := s.place(ctx, claims)
	if errors.Is(err, apierr.ErrForbidden) {
		// Said to the operator as well as to the person refused: the fix is
		// theirs or the identity provider's, never the person's. Neither the
		// subject nor the email is logged; the issuer and the claim say where
		// to look.
		log.Warn().
			Str("issuer", redaction.RedactText(claims.Issuer)).
			Str("claim", claims.OrganizationClaim).
			Str("reason", err.Error()).
			Msg("Refused a sign-in through the identity provider: it does not place the person in any organization here")
		s.placements.put(key, identityPlacement{}, err)
		return identityPlacement{}, err
	}
	if err != nil {
		return identityPlacement{}, err
	}
	s.placements.put(key, placed, nil)
	return placed, nil
}

// place finds the account an identity is linked to — creating it at the first
// sign-in — and makes it a member of exactly the organizations the claim names
// that exist here.
func (s *userService) place(ctx context.Context, claims entities.IdentityClaims) (identityPlacement, error) {
	if claims.Issuer == "" || claims.Subject == "" {
		// Without both there is no identity to link an account to.
		return identityPlacement{}, fmt.Errorf("%w: the ID token names no issuer or no subject", auth.ErrUnauthorized)
	}
	named, err := organizationsNamedBy(claims)
	if err != nil {
		return identityPlacement{}, err
	}
	organizations, err := s.repo.User().LiveOrganizations(ctx, named)
	if err != nil {
		return identityPlacement{}, fmt.Errorf("could not read the organizations the claim names: %w", err)
	}
	if len(organizations) == 0 {
		return identityPlacement{}, refusalNamingNoOrganization(claims.OrganizationClaim)
	}
	account, err := s.linkedAccount(ctx, claims, organizations)
	if err != nil {
		return identityPlacement{}, err
	}
	return identityPlacement{account: account, organizations: organizations}, nil
}

// linkedAccount returns the account linked to the claims' identity, a member of
// exactly organizations, creating it at the first sign-in.
func (s *userService) linkedAccount(ctx context.Context, claims entities.IdentityClaims, organizations []uuid.UUID) (uuid.UUID, error) {
	existing, err := s.repo.User().GetByIdentity(ctx, claims.Issuer, claims.Subject)
	if err == nil && holdsExactly(existing.Organizations, organizations) {
		// The usual case: the claim is what it was. Nothing to write, and no
		// lock taken on the account to find that out.
		return uuid.UUID(existing.ID), nil
	}
	if err == nil {
		return s.keepInStep(ctx, uuid.UUID(existing.ID), organizations)
	}
	if !errors.Is(err, apierr.ErrNotFound) {
		return uuid.Nil, fmt.Errorf("could not look up the account linked to this identity: %w", err)
	}
	return s.createLinkedAccount(ctx, claims, organizations)
}

// keepInStep makes a linked account's memberships the organizations its latest
// claim names.
//
// Every membership a linked account holds came from its claim: an
// administrator creates local accounts only, and nothing adds an existing
// account to an organization. So the claim's set replaces the account's —
// an organization the provider stopped naming is left, one it started naming is
// joined — and a local account is never touched, because only a sign-in through
// a provider reaches here. Whatever grants memberships by hand one day has to
// mark them, or the next sign-in will take them back.
func (s *userService) keepInStep(ctx context.Context, account uuid.UUID, organizations []uuid.UUID) (uuid.UUID, error) {
	changed, err := s.repo.User().SetOrganizations(ctx, account, organizations)
	if err != nil {
		return uuid.Nil, fmt.Errorf("could not place the account in its organizations: %w", err)
	}
	if changed {
		// The cached account carries the memberships it had.
		s.principals.forget(account)
	}
	return account, nil
}

// holdsExactly reports whether an account's memberships are the organizations
// wanted, in any order.
func holdsExactly(held []models.OrganizationModel, wanted []uuid.UUID) bool {
	if len(held) != len(wanted) {
		return false
	}
	want := make(map[uuid.UUID]bool, len(wanted))
	for _, id := range wanted {
		want[id] = true
	}
	for _, org := range held {
		if !want[uuid.UUID(org.ID)] {
			return false
		}
	}
	return true
}
