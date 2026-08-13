package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

// UserRepository defines the contract for user persistence.
type UserRepository interface {
	Get(ctx context.Context, id uuid.UUID) (models.UserModel, error)
	GetByUsername(ctx context.Context, username string) (models.UserModel, error)
	GetWithPasswordByUsername(ctx context.Context, username string) (models.UserModel, string, error)
	ListByOrganization(ctx context.Context, organizationID uuid.UUID) ([]models.UserModel, error)
	Create(ctx context.Context, u models.UserModel, passwordHash string) error

	// SetPasswordHash replaces one user's password hash and touches nothing
	// else. Update writes the whole row, so it cannot be used for this without
	// blanking whatever the caller did not supply.
	SetPasswordHash(ctx context.Context, id uuid.UUID, passwordHash string) error
	Update(ctx context.Context, u models.UserModel) error
	Delete(ctx context.Context, id uuid.UUID) error

	AddOrganization(ctx context.Context, userID, organizationID uuid.UUID) error
	RemoveOrganization(ctx context.Context, userID, organizationID uuid.UUID) error
	AddProject(ctx context.Context, userID, projectID uuid.UUID) error
	RemoveProject(ctx context.Context, userID, projectID uuid.UUID) error
}
