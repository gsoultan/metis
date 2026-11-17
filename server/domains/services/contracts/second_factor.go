package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// SecondFactorSignIn is a password sign-in and the steps after it: a code
// from the account's authenticator app, or setting one up first.
type SecondFactorSignIn interface {
	// SignIn checks a password and says what the sign-in still needs.
	SignIn(ctx context.Context, username, password string) (entities.SignIn, error)
	// SignInWithCode finishes a sign-in with a code from the authenticator
	// app, or a recovery code.
	SignInWithCode(ctx context.Context, challenge, code string) (entities.SignIn, error)
	// BeginEnrolment makes a new secret for the account a challenge is for.
	BeginEnrolment(ctx context.Context, challenge string) (entities.SecondFactorEnrolment, error)
	// ConfirmEnrolment finishes setting up the authenticator app with its
	// first code, and returns the session and the recovery codes, shown once.
	ConfirmEnrolment(ctx context.Context, challenge, code string) (entities.SignIn, []string, error)
}

// SecondFactorAccount is an account's authenticator app after sign-in.
type SecondFactorAccount interface {
	// SecondFactorStatus says whether the account signs in with one.
	SecondFactorStatus(ctx context.Context, userID uuid.UUID) (entities.SecondFactorStatus, error)
	// RegenerateRecoveryCodes replaces the account's recovery codes, given a
	// current code from its authenticator app.
	RegenerateRecoveryCodes(ctx context.Context, userID uuid.UUID, code string) ([]string, error)
	// ResetSecondFactor removes the authenticator app of an account the caller
	// administers, so its owner sets up a new one at their next sign-in.
	ResetSecondFactor(ctx context.Context, userID uuid.UUID) error
	// RemoveSecondFactor removes an account's authenticator app by username.
	// It is what --reset-two-factor runs on the server, for an installation
	// whose only administrator lost theirs; nothing else calls it.
	RemoveSecondFactor(ctx context.Context, username string) error
}
