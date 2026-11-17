package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// UserSecondFactorReader reads an account's authenticator.
type UserSecondFactorReader interface {
	// Get returns the account's authenticator, confirmed or begun, and false
	// when it has none.
	Get(ctx context.Context, userID uuid.UUID) (entities.UserSecondFactor, bool, error)
}

// UserSecondFactorEnroller begins, finishes and removes an enrolment.
type UserSecondFactorEnroller interface {
	// Begin records a new secret for an account with no confirmed
	// authenticator, replacing one begun before. apierr.ErrForbidden when the
	// account already has a confirmed one: that is replaced only by an
	// administrator removing it first.
	Begin(ctx context.Context, userID uuid.UUID, secret string) error
	// Confirm finishes the enrolment begun with secret, taking step as the
	// first code used and keeping the recovery codes' hashes. False when the
	// enrolment is no longer that one, or step was already taken.
	Confirm(ctx context.Context, userID uuid.UUID, secret string, step int64, recoveryHashes []string, at time.Time) (bool, error)
	// Delete removes the account's authenticator, confirmed or not. False
	// when it had none.
	Delete(ctx context.Context, userID uuid.UUID) (bool, error)
}

// UserSecondFactorSpender uses up what a confirmed authenticator signs in
// with, each thing once.
type UserSecondFactorSpender interface {
	// TakeStep records step as used. False when it, or a later one, already
	// was, or the account has no confirmed authenticator.
	TakeStep(ctx context.Context, userID uuid.UUID, step int64) (bool, error)
	// TakeRecoveryCode removes one unused recovery code by its hash. False
	// when the account holds no such code.
	TakeRecoveryCode(ctx context.Context, userID uuid.UUID, hash string) (bool, error)
	// ReplaceRecoveryCodes sets a new list in place of the old one.
	ReplaceRecoveryCodes(ctx context.Context, userID uuid.UUID, hashes []string) error
}

// UserSecondFactorRepository keeps staff accounts' authenticators in the main
// database, beside the accounts.
type UserSecondFactorRepository interface {
	UserSecondFactorReader
	UserSecondFactorEnroller
	UserSecondFactorSpender
}
