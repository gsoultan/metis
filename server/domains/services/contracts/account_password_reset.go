package contracts

import (
	"context"

	"github.com/google/uuid"
)

// AccountPasswordReset is an administrator setting somebody else's password
// from the account form: the answer for a person who forgot theirs, on an
// installation that is still running.
type AccountPasswordReset interface {
	// ResetPassword replaces another account's password and signs it out
	// everywhere. The caller has to administer every organization the account
	// belongs to — and be a platform administrator when the account holds a
	// role in every organization. Their own password is refused here: that
	// goes through ChangePassword, which asks for the current one.
	ResetPassword(ctx context.Context, userID uuid.UUID, newPassword string) error
}
