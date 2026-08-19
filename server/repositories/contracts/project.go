package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

// ProjectRepository defines the methods to interact with projects.
type ProjectRepository interface {
	Get(ctx context.Context, id uuid.UUID) (models.ProjectModel, error)
	List(ctx context.Context) ([]models.ProjectModel, error)
	ListByOrganization(ctx context.Context, organizationID uuid.UUID) ([]models.ProjectModel, error)
	Create(ctx context.Context, p models.ProjectModel) error
	Update(ctx context.Context, p models.ProjectModel) error
	Delete(ctx context.Context, id uuid.UUID) error
}
