package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

type DeploymentService interface {
	Deploy(ctx context.Context, projectID uuid.UUID, name string, resources []entities.Resource) (entities.Deployment, error)
	GetDeployment(ctx context.Context, id uuid.UUID) (entities.Deployment, error)
	ListDeployments(ctx context.Context, projectID uuid.UUID) ([]entities.Deployment, error)
}
