package impl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/internal/pkg/totp"
	"github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// authenticatorIssuer is the name an authenticator app shows beside the code.
const authenticatorIssuer = "Metis"

// ErrSecondFactorRequired refuses a password-only sign-in for an account a
// code is asked of. An integration signs in with an API key instead.
var ErrSecondFactorRequired = fmt.Errorf(
	"%w: this account signs in with a code from an authenticator app as well as its password; "+
		"a program should use an API key", auth.ErrAuthenticationFailed)

// errSessionWithoutCode refuses a session minted from a password alone where
// every sign-in asks for a code: one from before two-factor sign-in was
// required, which its owner replaces by signing in again.
var errSessionWithoutCode = errors.New("invalid token: two-factor sign-in is required; sign in again")

// SignIn checks a password, then gives a session, or a challenge for the step
// that has to come first: a code, from an account with an authenticator app,
// or setting one up, from an account without one where every account is asked.
func (s *userService) SignIn(ctx context.Context, username, password string) (entities.SignIn, error) {
	u, err := s.checkPassword(ctx, username, password)
	if err != nil {
		return entities.SignIn{}, err
	}
	factor, found, err := s.repo.UserSecondFactor().Get(ctx, u.ID)
	if err != nil {
		return entities.SignIn{}, err
	}
	next := entities.SignInDone
	switch {
	case found && factor.Confirmed():
		next = entities.SignInCode
	case s.secondFactor == SecondFactorRequired:
		next = entities.SignInEnrol
	}
	if next == entities.SignInDone {
		token, err := s.mintSession(u, methodPassword)
		return entities.SignIn{User: u, Token: token}, err
	}
	challenge, expires, err := s.mintChallenge(u.ID, next, time.Now())
	if err != nil {
		return entities.SignIn{}, err
	}
	return entities.SignIn{User: u, Next: next, Challenge: challenge, ChallengeExpiresAt: expires}, nil
}

// SignInWithCode finishes a sign-in with a code from the authenticator app, or
// one of the account's recovery codes, each usable once.
func (s *userService) SignInWithCode(ctx context.Context, challenge, code string) (entities.SignIn, error) {
	account, err := s.challengedAccount(ctx, challenge, entities.SignInCode)
	if err != nil {
		return entities.SignIn{}, err
	}
	u := adapters.UserEntityAdapter{Model: account}.ToEntity()
	byRecovery, err := s.spendCode(ctx, u, code, true)
	if err != nil {
		return entities.SignIn{}, err
	}
	if byRecovery {
		held := rolesOf(account)
		traceAccountChange(actingAs(ctx, u), accountRecoveryCodeUsed, u.ID, u.Username, held, held)
	}
	token, err := s.mintSession(u, methodPassword, methodCode)
	return entities.SignIn{User: u, Token: token}, err
}

// BeginEnrolment makes a new secret for the account a challenge is for, and
// shows it once. Beginning again replaces a secret not yet confirmed: a QR
// code scanned on the wrong phone is not a dead end.
func (s *userService) BeginEnrolment(ctx context.Context, challenge string) (entities.SecondFactorEnrolment, error) {
	account, err := s.challengedAccount(ctx, challenge, entities.SignInEnrol)
	if err != nil {
		return entities.SecondFactorEnrolment{}, err
	}
	secret, err := totp.NewSecret()
	if err != nil {
		return entities.SecondFactorEnrolment{}, err
	}
	sealed, err := crypto.Encrypt(secret)
	if err != nil {
		return entities.SecondFactorEnrolment{}, fmt.Errorf("could not seal the secret: %w", err)
	}
	if err := s.repo.UserSecondFactor().Begin(ctx, uuid.UUID(account.ID), sealed); err != nil {
		return entities.SecondFactorEnrolment{}, err
	}
	return entities.SecondFactorEnrolment{Secret: secret, URI: totp.URI(authenticatorIssuer, account.Username, secret)}, nil
}

// ConfirmEnrolment takes the first code from the new authenticator app, which
// proves it was set up, and gives the session and the recovery codes.
//
// Every session the account had before ends: they were signed in without the
// code, and somebody who had the password alone could have been holding one.
func (s *userService) ConfirmEnrolment(ctx context.Context, challenge, code string) (entities.SignIn, []string, error) {
	account, err := s.challengedAccount(ctx, challenge, entities.SignInEnrol)
	if err != nil {
		return entities.SignIn{}, nil, err
	}
	u := adapters.UserEntityAdapter{Model: account}.ToEntity()
	codes, err := s.confirmFirstCode(ctx, u, code)
	if err != nil {
		return entities.SignIn{}, nil, err
	}
	if err := s.repo.User().EndSessions(ctx, u.ID, time.Now().Truncate(time.Second)); err != nil {
		return entities.SignIn{}, nil, err
	}
	s.principals.forget(u.ID)
	held := rolesOf(account)
	traceAccountChange(actingAs(ctx, u), accountSecondFactorEnrolled, u.ID, u.Username, held, held)
	token, err := s.mintSession(u, methodPassword, methodCode)
	return entities.SignIn{User: u, Token: token}, codes, err
}

// confirmFirstCode checks the first code of an enrolment begun and not
// finished, and confirms the enrolment of the secret the code came from,
// returning the recovery codes it was given.
func (s *userService) confirmFirstCode(ctx context.Context, u entities.User, code string) ([]string, error) {
	if err := s.waitForCodes(u.ID); err != nil {
		return nil, err
	}
	factor, found, err := s.repo.UserSecondFactor().Get(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if !found || factor.Confirmed() {
		return nil, errChallenge
	}
	step, ok, err := matchAppCode(factor, code)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, s.wrongCode(u.ID)
	}
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		return nil, err
	}
	confirmed, err := s.repo.UserSecondFactor().Confirm(ctx, u.ID, factor.Secret, step, hashes, time.Now())
	if err != nil {
		return nil, err
	}
	if !confirmed {
		// Another enrolment began, or this code was taken, between the read
		// and the write.
		return nil, s.wrongCode(u.ID)
	}
	s.codes.Succeeded(u.ID.String())
	return codes, nil
}

// challengedAccount reads the account a live challenge for step is for. A
// challenge issued before the account's credentials last changed — a password
// reset, an authenticator removed — is refused, as a session would be.
func (s *userService) challengedAccount(ctx context.Context, challenge string, step entities.SignInStep) (models.UserModel, error) {
	userID, issued, err := s.readChallenge(challenge, step)
	if err != nil {
		return models.UserModel{}, err
	}
	account, _, err := s.repo.User().GetWithPasswordByID(ctx, userID)
	if err != nil {
		return models.UserModel{}, errChallenge
	}
	if cutoff := account.TokensValidFrom; cutoff != nil && issued.Unix() < cutoff.Unix() {
		return models.UserModel{}, errChallenge
	}
	return account, nil
}
