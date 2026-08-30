package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// GroupService defines the group management operations.
type GroupService interface {
	ListGroups(ctx context.Context, organizationID uuid.UUID) ([]entities.Group, error)
	CreateGroup(ctx context.Context, g entities.Group) error
	GetGroup(ctx context.Context, id uuid.UUID) (entities.Group, error)
	UpdateGroup(ctx context.Context, g entities.Group) error
	DeleteGroup(ctx context.Context, id uuid.UUID) error
	ListGroupMembers(ctx context.Context, groupID uuid.UUID) ([]entities.User, error)
	AddMembership(ctx context.Context, userID, groupID uuid.UUID) error
	RemoveMembership(ctx context.Context, userID, groupID uuid.UUID) error
	ListUserGroups(ctx context.Context, userID uuid.UUID) ([]entities.Group, error)
}
