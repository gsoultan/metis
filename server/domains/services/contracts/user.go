package contracts

import (
	"context"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

// UserService defines the user and group management operations.
type UserService interface {
	GetUser(ctx context.Context, id uuid.UUID) (entities.User, error)
	GetUserByUsername(ctx context.Context, username string) (entities.User, error)
	ListUsers(ctx context.Context, organizationID uuid.UUID) ([]entities.User, error)
	CreateUser(ctx context.Context, u entities.User, password string) error
	UpdateUser(ctx context.Context, u entities.User) error
	DeleteUser(ctx context.Context, id uuid.UUID) error

	// SetPassword replaces one account's password, identified by username so
	// that an operator who has been locked out can name the account they know.
	SetPassword(ctx context.Context, username, newPassword string) error

	Login(ctx context.Context, username, password string) (entities.User, string, error) // Returns user and JWT token
	ValidateToken(ctx context.Context, token string) (entities.User, error)

	AssignOrganization(ctx context.Context, userID, organizationID uuid.UUID) error
	UnassignOrganization(ctx context.Context, userID, organizationID uuid.UUID) error
	AssignProject(ctx context.Context, userID, projectID uuid.UUID) error
	UnassignProject(ctx context.Context, userID, projectID uuid.UUID) error
}
