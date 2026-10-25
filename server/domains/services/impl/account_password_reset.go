package impl

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"golang.org/x/crypto/bcrypt"
)

// maxPasswordBytes is the most bcrypt reads of a password. It refuses a longer
// one outright, which would otherwise surface as a failure to hash rather than
// as something the administrator can fix.
const maxPasswordBytes = 72

// ResetPassword sets the password of an account the caller administers, for
// the person who forgot theirs.
//
// Until this, the only answer on a running installation was --reset-password
// on the server, so an administrator who could create an account with a
// password could not give a colleague a new one.
//
// What it asks of the caller is what deleting the account asks, since taking
// its sign-in is as much as removing it: authority over every organization the
// account belongs to, and the platform's when the account holds a role in
// every organization — otherwise the administrator of one organization could
// take the sign-in of somebody who administers all of them. The caller's own
// account is refused: ChangePassword asks for the current password, and this
// would be the way round it for a stolen session. An account an identity
// provider signs in is refused as SetPassword refuses it.
//
// The new password ends every session the account had, as any password change
// does, and the change is logged with who made it. The password is not.
func (s *userService) ResetPassword(ctx context.Context, userID uuid.UUID, newPassword string) error {
	if err := checkNewPassword(newPassword); err != nil {
		return err
	}
	stored, err := s.repo.User().Get(ctx, userID)
	if err != nil {
		return err
	}
	if err := requireAccountAuthority(ctx, stored); err != nil {
		return err
	}
	if caller := signedIn(ctx); caller != nil && caller.ID == userID {
		return apierr.Forbiddenf("change your own password from your profile, which asks for the current one")
	}
	if issuer := stored.IdentityIssuer; issuer != nil && *issuer != "" {
		return apierr.Invalidf("%s signs in through the identity provider %s, which holds the password; reset it there",
			stored.Username, *issuer)
	}
	if holdsBuiltInRole(stored.Roles) {
		if err := s.requireGlobalRoleAuthority(ctx); err != nil {
			return err
		}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("could not hash the new password: %w", err)
	}
	if err := s.repo.User().SetPasswordHash(ctx, userID, string(hash)); err != nil {
		return err
	}
	s.principals.forget(userID)
	held := rolesOf(stored)
	traceAccountChange(ctx, accountResetByAdministrator, userID, stored.Username, held, held)
	return nil
}

// checkNewPassword refuses a password too short to replace one, or too long
// for bcrypt to read whole.
func checkNewPassword(password string) error {
	if len(strings.TrimSpace(password)) < MinPasswordLength {
		return apierr.Invalidf("password must be at least %d characters", MinPasswordLength)
	}
	if len(password) > maxPasswordBytes {
		return apierr.Invalidf("password must be at most %d bytes", maxPasswordBytes)
	}
	return nil
}
