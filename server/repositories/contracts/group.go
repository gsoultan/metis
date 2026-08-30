package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// GroupRepository defines the contract for group persistence.
type GroupRepository interface {
	List(ctx context.Context, organizationID uuid.UUID) ([]models.GroupModel, error)
	Create(ctx context.Context, g models.GroupModel) error
	Get(ctx context.Context, id uuid.UUID) (models.GroupModel, error)
	Update(ctx context.Context, g models.GroupModel) error
	Delete(ctx context.Context, id uuid.UUID) error
	ListGroupMembers(ctx context.Context, groupID uuid.UUID) ([]models.UserModel, error)
	AddMembership(ctx context.Context, userID, groupID uuid.UUID) error
	RemoveMembership(ctx context.Context, userID, groupID uuid.UUID) error
	ListUserGroups(ctx context.Context, userID uuid.UUID) ([]models.GroupModel, error)
}
