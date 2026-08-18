package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

// ProjectService defines the project-related operations.
type ProjectService interface {
	CreateProject(ctx context.Context, organizationID uuid.UUID, name, description string) (entities.Project, error)
	GetProject(ctx context.Context, id uuid.UUID) (entities.Project, error)
	ListProjects(ctx context.Context, organizationID uuid.UUID) ([]entities.Project, error)
	UpdateProject(ctx context.Context, id uuid.UUID, organizationID uuid.UUID, name, description string) error
	DeleteProject(ctx context.Context, id uuid.UUID) error
	GetProcessStatistics(ctx context.Context, projectID uuid.UUID) (entities.ProcessStatistics, error)
}
