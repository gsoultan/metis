package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

// OrganizationRepository defines the methods to interact with organizations.
type OrganizationRepository interface {
	Get(ctx context.Context, id uuid.UUID) (models.OrganizationModel, error)
	List(ctx context.Context) ([]models.OrganizationModel, error)
	Create(ctx context.Context, o models.OrganizationModel) error
	Update(ctx context.Context, o models.OrganizationModel) error
	Delete(ctx context.Context, id uuid.UUID) error
}
