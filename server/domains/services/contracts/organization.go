package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// OrganizationService defines the organization-related operations.
type OrganizationService interface {
	CreateOrganization(ctx context.Context, name, description string) (entities.Organization, error)
	GetOrganization(ctx context.Context, id uuid.UUID) (entities.Organization, error)
	ListOrganizations(ctx context.Context) ([]entities.Organization, error)
	UpdateOrganization(ctx context.Context, id uuid.UUID, name, description string) error
	DeleteOrganization(ctx context.Context, id uuid.UUID) error
}
