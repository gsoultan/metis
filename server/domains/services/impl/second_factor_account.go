package impl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
)

// SecondFactorStatus says whether an account signs in with an authenticator
// app, and how many recovery codes it has left.
func (s *userService) SecondFactorStatus(ctx context.Context, userID uuid.UUID) (entities.SecondFactorStatus, error) {
	status := entities.SecondFactorStatus{Required: s.secondFactor == SecondFactorRequired}
	factor, found, err := s.repo.UserSecondFactor().Get(ctx, userID)
	if err != nil || !found || !factor.Confirmed() {
		return status, err
	}
	status.Enrolled = true
	status.ConfirmedAt = factor.ConfirmedAt
	status.RecoveryCodesLeft = len(factor.RecoveryCodeHashes)
	return status, nil
}

// RegenerateRecoveryCodes replaces an account's recovery codes, given a code
// from its authenticator app — not a recovery code: somebody holding a stolen
// session and one stolen recovery code must not be able to mint more.
func (s *userService) RegenerateRecoveryCodes(ctx context.Context, userID uuid.UUID, code string) ([]string, error) {
	u, err := s.ownAccount(ctx, userID)
	if err != nil {
		return nil, err
	}
	status, err := s.SecondFactorStatus(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !status.Enrolled {
		return nil, fmt.Errorf("%w: this account has no authenticator app", apierr.ErrNotFound)
	}
	if _, err := s.spendCode(ctx, u, code, false); err != nil {
		return nil, err
	}
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	if err := s.repo.UserSecondFactor().ReplaceRecoveryCodes(ctx, userID, hashes); err != nil {
		return nil, err
	}
	traceAccountChange(ctx, accountRecoveryCodesReplaced, userID, u.Username, accountRoles{}, accountRoles{})
	return codes, nil
}

// ResetSecondFactor removes the authenticator app of an account the caller
// administers. Its owner's sessions end, and their next sign-in sets up a new
// one. It is what an administrator does for somebody who lost their phone
// and their recovery codes.
//
// Never for the caller's own account: a stolen session must not be able to
// take its owner's second factor away. Another administrator does it.
func (s *userService) ResetSecondFactor(ctx context.Context, userID uuid.UUID) error {
	stored, err := s.repo.User().Get(ctx, userID)
	if err != nil {
		return err
	}
	if err := requireAccountAuthority(ctx, stored); err != nil {
		return err
	}
	if caller := signedIn(ctx); caller != nil && caller.ID == userID {
		return apierr.Forbiddenf("another administrator has to remove your own authenticator app")
	}
	if holdsBuiltInRole(stored.Roles) {
		if err := s.requireGlobalRoleAuthority(ctx); err != nil {
			return err
		}
	}
	removed, err := s.repo.UserSecondFactor().Delete(ctx, userID)
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("%w: %s has no authenticator app", apierr.ErrNotFound, stored.Username)
	}
	// A second later than now, as signing out is: a session minted in this
	// second with the code that just stopped counting must end too.
	if err := s.repo.User().EndSessions(ctx, userID, time.Now().Truncate(time.Second).Add(time.Second)); err != nil {
		return err
	}
	s.principals.forget(userID)
	held := rolesOf(stored)
	traceAccountChange(ctx, accountSecondFactorReset, userID, stored.Username, held, held)
	return nil
}

// RemoveSecondFactor removes an account's authenticator app, named by
// username, and ends its sessions. Only --reset-two-factor calls it: whoever
// can run it on the server already holds what a backup restore would, so it
// asks nothing more, and the log says it was done there.
func (s *userService) RemoveSecondFactor(ctx context.Context, username string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("a username is required")
	}
	account, err := s.repo.User().GetByUsername(ctx, username)
	if err != nil {
		return fmt.Errorf("no such user %q", username)
	}
	userID := uuid.UUID(account.ID)
	removed, err := s.repo.UserSecondFactor().Delete(ctx, userID)
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("%q has no authenticator app; their next sign-in sets one up", username)
	}
	if err := s.repo.User().EndSessions(ctx, userID, time.Now().Truncate(time.Second).Add(time.Second)); err != nil {
		return err
	}
	s.principals.forget(userID)
	held := rolesOf(account)
	traceUnattended(madeThroughResetTwoFactor, accountSecondFactorReset, uuid.Nil, userID, account.Username, held, held)
	return nil
}

// ownAccount reads the signed-in account by id, as an entity.
func (s *userService) ownAccount(ctx context.Context, userID uuid.UUID) (entities.User, error) {
	m, _, err := s.repo.User().GetWithPasswordByID(ctx, userID)
	if err != nil {
		return entities.User{}, err
	}
	return adapters.UserEntityAdapter{Model: m}.ToEntity(), nil
}
