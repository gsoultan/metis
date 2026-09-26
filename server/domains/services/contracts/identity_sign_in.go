package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// IdentityProviderSignIn resolves somebody an identity provider vouched for to
// the account they act as here.
type IdentityProviderSignIn interface {
	// SignInThroughIdentityProvider returns the account linked to the claims'
	// issuer and subject — creating it at the first sign-in — as a member of
	// exactly the organizations the claims name. The account is the principal;
	// the claims are only evidence. Somebody the claims place in no
	// organization is refused with an apierr.ErrForbidden error that says
	// which setting or claim is missing.
	SignInThroughIdentityProvider(ctx context.Context, claims entities.IdentityClaims) (entities.User, error)
}
