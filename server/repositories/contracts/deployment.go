package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

type DeploymentRepository interface {
	Create(ctx context.Context, d models.DeploymentModel) error
	Get(ctx context.Context, id uuid.UUID) (models.DeploymentModel, error)
	ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.DeploymentModel, error)
	GetResource(ctx context.Context, id uuid.UUID) (models.ResourceModel, error)
	ListResources(ctx context.Context, deploymentID uuid.UUID) ([]models.ResourceModel, error)
}
