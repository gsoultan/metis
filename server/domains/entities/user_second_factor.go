package entities

import (
	"time"

	"github.com/google/uuid"
)

// UserSecondFactor is a staff account's authenticator app.
//
// Secret is as stored: sealed with ENCRYPTION_KEY. Only the sign-in opens it,
// to check a code, and only the enrolment shows it, once. ConfirmedAt is nil
// while an enrolment is begun and not finished. RecoveryCodeHashes are the
// SHA-256 of the recovery codes not yet used.
type UserSecondFactor struct {
	UserID             uuid.UUID
	Secret             string
	ConfirmedAt        *time.Time
	LastStep           int64
	RecoveryCodeHashes []string
}

// Confirmed reports whether the account signs in with it.
func (f UserSecondFactor) Confirmed() bool { return f.ConfirmedAt != nil }
