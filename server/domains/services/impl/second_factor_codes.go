package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/internal/pkg/totp"
	"github.com/gsoultan/metis/server/domains/entities"
)

// errWrongCode is the refusal of a code that is not right, or not right now.
var errWrongCode = fmt.Errorf("%w: the code is not right; check the time on your phone, or use a recovery code",
	auth.ErrAuthenticationFailed)

// spendCode takes a code from the account's confirmed authenticator app, or,
// when recovery allows it, one of its recovery codes, and uses it up. It
// reports whether it was a recovery code.
func (s *userService) spendCode(ctx context.Context, u entities.User, code string, recovery bool) (bool, error) {
	if err := s.waitForCodes(u.ID); err != nil {
		return false, err
	}
	factor, found, err := s.repo.UserSecondFactor().Get(ctx, u.ID)
	if err != nil {
		return false, err
	}
	if !found || !factor.Confirmed() {
		return false, errChallenge
	}
	byRecovery := recovery && looksLikeRecoveryCode(code)
	var taken bool
	if byRecovery {
		taken, err = s.repo.UserSecondFactor().TakeRecoveryCode(ctx, u.ID, hashRecoveryCode(code))
	} else {
		taken, err = s.takeAppCode(ctx, factor, code)
	}
	if err != nil {
		return false, err
	}
	if !taken {
		return false, s.wrongCode(u.ID)
	}
	s.codes.Succeeded(u.ID.String())
	return byRecovery, nil
}

// takeAppCode takes a code from the authenticator app, once: the step it was
// made in is recorded, and no code from that step or an earlier one is taken
// again — read over a shoulder, or sent twice by a double click.
func (s *userService) takeAppCode(ctx context.Context, factor entities.UserSecondFactor, code string) (bool, error) {
	step, ok, err := matchAppCode(factor, code)
	if err != nil || !ok {
		return false, err
	}
	return s.repo.UserSecondFactor().TakeStep(ctx, factor.UserID, step)
}

// matchAppCode reports the step a code from the app was made in, if it is the
// code of a step near now and later than the last one taken.
func matchAppCode(factor entities.UserSecondFactor, code string) (int64, bool, error) {
	secret, err := crypto.Decrypt(factor.Secret)
	if err != nil {
		return 0, false, fmt.Errorf("could not open the authenticator's secret: %w", err)
	}
	step, ok := totp.Match(secret, code, time.Now(), factor.LastStep)
	return step, ok, nil
}

// waitForCodes refuses a code while the account is being made to wait after
// wrong ones. Six digits are a million guesses; without this, a challenge's
// five minutes would allow most of them.
func (s *userService) waitForCodes(userID uuid.UUID) error {
	if retry, wait := s.codes.RetryAfter(userID.String(), time.Now()); wait {
		return fmt.Errorf("%w: too many wrong codes, try again in %s",
			auth.ErrAuthenticationFailed, retry.Round(time.Second))
	}
	return nil
}

// wrongCode counts a wrong code against the account and refuses it.
func (s *userService) wrongCode(userID uuid.UUID) error {
	s.codes.Failed(userID.String(), time.Now())
	return errWrongCode
}

// actingAs is ctx with u as the actor, for the log line a sign-in writes
// before anybody is signed in.
func actingAs(ctx context.Context, u entities.User) context.Context {
	return context.WithValue(ctx, auth.UserContextKey, u)
}
