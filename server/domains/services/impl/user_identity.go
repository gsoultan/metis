package impl

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

// LocalUserIDFromContext returns the ID of the signed-in local account.
//
// It deliberately refuses an OIDC principal rather than falling back to some
// other identifier. An OIDC user's password lives at the identity provider;
// Metis holds no hash that logging in ever consults. Letting them "change their
// password" here would rotate a value that gates nothing, and report success —
// so a user who believed they had locked an attacker out would not have. The
// honest answer is that this is the wrong place to do it.
func LocalUserIDFromContext(ctx context.Context) (uuid.UUID, error) {
	switch u := ctx.Value(pkgauth.UserContextKey).(type) {
	case entities.User:
		return u.ID, nil
	case *entities.User:
		if u != nil {
			return u.ID, nil
		}
	case pkgauth.UserClaims, *pkgauth.UserClaims:
		return uuid.Nil, apierr.Invalidf(
			"this account signs in through your identity provider, so its password is not stored here — change it there")
	}
	return uuid.Nil, pkgauth.ErrUnauthorized
}
