package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// ProfileService is what a signed-in person may do to their own account.
//
// Apart from UserService's administrative methods, which can change roles: the
// account's owner may read it and change how they are named and reached, and
// nothing else. The account is the one the session names — callers pass the
// ID from the verified token, never one from the request.
type ProfileService interface {
	GetOwnProfile(ctx context.Context, userID uuid.UUID) (entities.User, error)
	UpdateOwnProfile(ctx context.Context, userID uuid.UUID, profile entities.Profile) error
}
