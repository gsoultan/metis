package contracts

import (
	"context"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

// ProcessRepository defines the BPM process instance operations.
type ProcessRepository interface {
	Create(ctx context.Context, instance models.ProcessInstanceModel) (uuid.UUID, error)
	Get(ctx context.Context, id uuid.UUID) (models.ProcessInstanceModel, error)
	GetForUpdate(ctx context.Context, id uuid.UUID) (models.ProcessInstanceModel, error)
	Update(ctx context.Context, instance models.ProcessInstanceModel) error
	List(ctx context.Context) ([]models.ProcessInstanceModel, error)
	ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.ProcessInstanceModel, error)

	// Paged variants for the instance list a user browses.
	ListPaged(ctx context.Context, p Pagination) (Page[models.ProcessInstanceModel], error)
	ListByProjectPaged(ctx context.Context, projectID uuid.UUID, p Pagination) (Page[models.ProcessInstanceModel], error)
	ListByDefinition(ctx context.Context, definitionID uuid.UUID) ([]models.ProcessInstanceModel, error)
	ListByParent(ctx context.Context, parentInstanceID uuid.UUID) ([]models.ProcessInstanceModel, error)
	CountByStatus(ctx context.Context, projectID uuid.UUID, status models.ProcessStatus) (int64, error)
}
