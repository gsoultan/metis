package impl

import (
	"context"
	"time"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
)

// SignOut ends every session the caller's account holds.
//
// Through the credential cutoff a password change already moves, so it costs
// no table and no read per request: the cutoff travels with the cached
// principal. Issued-at is in whole seconds and a token minted in the cutoff's
// own second is let through, so the cutoff is the start of the next second: a
// token issued in the second of the sign-out is refused with the rest, and a
// sign-in from the next second on works. Every device is signed out: refusing
// one token alone would need a list of refused tokens read on every request.
func (s *userService) SignOut(ctx context.Context) error {
	caller := signedIn(ctx)
	if caller == nil {
		return pkgauth.ErrUnauthorized
	}
	if caller.IdentityProvider != "" {
		return apierr.Invalidf("this account signs in through your identity provider; sign out there")
	}
	if err := s.repo.User().EndSessions(ctx, caller.ID, time.Now().Truncate(time.Second).Add(time.Second)); err != nil {
		return err
	}
	s.principals.forget(caller.ID)
	return nil
}
