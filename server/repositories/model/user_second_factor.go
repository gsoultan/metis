package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// UserSecondFactor is an account's authenticator app: the second thing a
// password sign-in asks for.
//
// One row per account. A row with no ConfirmedAt is an enrolment begun and not
// finished — the secret was shown and no code from it has come back yet — and
// is replaced by the next one begun.
//
// Secret is sealed with ENCRYPTION_KEY, as a connector's secrets are: it is a
// password that never changes, and a backup must not be enough to make codes.
// LastStep is the last thirty-second step a code was taken from, so a code
// read over somebody's shoulder cannot be used again. RecoveryCodes holds the
// SHA-256 of each unused recovery code; using one removes it.
type UserSecondFactor struct {
	storm.Model

	User User

	Secret        string
	ConfirmedAt   *time.Time
	LastStep      int64
	RecoveryCodes storm.JSON
}

func (f *UserSecondFactor) Schema(t *storm.Table) {
	t.Name("user_second_factors")
	t.Col(&f.User).OnDelete(storm.Cascade)
	t.Col(&f.Secret).Size(512)
	t.Col(&f.RecoveryCodes).Default(emptyJSONList)
	// One authenticator per account.
	t.Index(&f.User).Unique().Named("ux_user_second_factors_user_id")
}
