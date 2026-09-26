package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// UserRepository defines the contract for user persistence.
type UserRepository interface {
	// UserIdentityRepository is the part a sign-in through an identity
	// provider uses: the account an identity is linked to, and where it is
	// placed.
	UserIdentityRepository

	Get(ctx context.Context, id uuid.UUID) (models.UserModel, error)
	GetByUsername(ctx context.Context, username string) (models.UserModel, error)
	GetWithPasswordByUsername(ctx context.Context, username string) (models.UserModel, string, error)

	// GetWithPasswordByID is the same lookup keyed by ID, for when the caller
	// is already authenticated and the session says who they are. Changing
	// one's own password uses this: taking the username from the request body
	// instead would let any signed-in user name somebody else.
	GetWithPasswordByID(ctx context.Context, id uuid.UUID) (models.UserModel, string, error)
	ListByOrganization(ctx context.Context, organizationID uuid.UUID) ([]models.UserModel, error)

	// HasAnotherAdministrator reports whether an organization has a member,
	// other than except, who holds the administrator role. It asks about every
	// member, so an organization of any size gets the same answer.
	HasAnotherAdministrator(ctx context.Context, organizationID, except uuid.UUID) (bool, error)

	// HasAccounts reports whether any account exists at all.
	//
	// Installation-wide, like the lookup by username, and for a related reason:
	// it answers whether this database already holds an installation, which
	// setup has to know before there is anybody to scope the question by.
	HasAccounts(ctx context.Context) (bool, error)
	Create(ctx context.Context, u models.UserModel, passwordHash string) error

	// SetPasswordHash replaces one user's password hash and touches nothing
	// else. Update writes the whole row, so it cannot be used for this without
	// blanking whatever the caller did not supply.
	SetPasswordHash(ctx context.Context, id uuid.UUID, passwordHash string) error

	// SetProfile writes an account's full name, display name and email — read
	// from u, with u.ID naming the account — and touches nothing else. For the
	// account's owner: Update writes every column from what its caller read,
	// roles included.
	SetProfile(ctx context.Context, u models.UserModel) error
	Update(ctx context.Context, u models.UserModel) error
	Delete(ctx context.Context, id uuid.UUID) error

	AddOrganization(ctx context.Context, userID, organizationID uuid.UUID) error
	RemoveOrganization(ctx context.Context, userID, organizationID uuid.UUID) error
	AddProject(ctx context.Context, userID, projectID uuid.UUID) error
	RemoveProject(ctx context.Context, userID, projectID uuid.UUID) error
}
