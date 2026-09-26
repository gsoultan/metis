package impl

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
)

// LocalUserIDFromContext returns the ID of the signed-in account, when its
// password is held here.
//
// It deliberately refuses an account that signs in through an identity
// provider rather than returning its ID. That person's password lives at the
// provider; Metis holds no hash that signing in ever consults. Letting them
// "change their password" here would rotate a value that gates nothing, and
// report success — so a user who believed they had locked an attacker out
// would not have. The honest answer is that this is the wrong place to do it.
func LocalUserIDFromContext(ctx context.Context) (uuid.UUID, error) {
	caller := signedIn(ctx)
	if caller == nil {
		return uuid.Nil, pkgauth.ErrUnauthorized
	}
	if caller.IdentityProvider != "" {
		return uuid.Nil, apierr.Invalidf(
			"this account signs in through your identity provider, so its password is not stored here — change it there")
	}
	return caller.ID, nil
}

// callerRoles returns the roles of whoever a request is from, and false when it
// carries nobody. Both ways of signing in put an account there, so these are
// always the roles an administrator granted.
func callerRoles(ctx context.Context) ([]string, bool) {
	if caller := signedIn(ctx); caller != nil {
		return caller.Roles, true
	}
	return nil, false
}

// callerOrganizations returns the organizations whoever a request is from
// belongs to, and false when it carries nobody — system work, a maintenance
// command. For an account signed in through an identity provider they are the
// organizations its token's claim names.
func callerOrganizations(ctx context.Context) (map[uuid.UUID]bool, bool) {
	caller := signedIn(ctx)
	if caller == nil {
		return nil, false
	}
	memberships := make(map[uuid.UUID]bool, len(caller.Organizations))
	for _, org := range caller.Organizations {
		if org != nil {
			memberships[org.ID] = true
		}
	}
	return memberships, true
}

// signedIn returns the account a request is from, or nil when it carries
// nobody. An account is the only principal either way of signing in leaves.
func signedIn(ctx context.Context) *entities.User {
	switch u := ctx.Value(pkgauth.UserContextKey).(type) {
	case entities.User:
		return &u
	case *entities.User:
		return u
	}
	return nil
}
