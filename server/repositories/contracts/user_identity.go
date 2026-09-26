package contracts

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// ErrAccountTaken refuses a new linked account whose username is already
// somebody's, or whose identity another account has just been linked to.
var ErrAccountTaken = errors.New("the account's username or identity is taken")

// UserIdentityRepository is what signing in through an identity provider needs
// of the account store: the account an identity is linked to, and the
// organizations it is placed in.
//
// Installation-wide, like the lookup by username and for the same reason: it
// runs while a sign-in is being resolved, before there is a tenant to scope by.
// And it reads and writes the main database whichever port a request arrived
// on, because that is where accounts live.
type UserIdentityRepository interface {
	// GetByIdentity returns the live account linked to (issuer, subject), or an
	// apierr.ErrNotFound error when there is none.
	GetByIdentity(ctx context.Context, issuer, subject string) (models.UserModel, error)

	// CreateLinked creates an account that signs in through (issuer, subject)
	// and holds no password, as a member of the organizations u names — all of
	// it in one transaction. A clash is refused with an error wrapping
	// ErrAccountTaken.
	CreateLinked(ctx context.Context, u models.UserModel, issuer, subject string) (models.UserModel, error)

	// LiveOrganizations returns, in the order given, the ids that name an
	// organization which exists and has not been deleted.
	LiveOrganizations(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error)

	// SetOrganizations makes an account a member of exactly the organizations
	// named, in one transaction, and reports whether that changed anything.
	SetOrganizations(ctx context.Context, userID uuid.UUID, organizationIDs []uuid.UUID) (bool, error)
}
