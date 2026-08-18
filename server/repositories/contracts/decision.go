package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

// DecisionRepository defines the decision definition operations.
type DecisionRepository interface {
	Get(ctx context.Context, id uuid.UUID) (models.DecisionDefinitionModel, error)
	GetByKey(ctx context.Context, key string) (models.DecisionDefinitionModel, error)
	GetByKeyAndVersion(ctx context.Context, key string, version int) (models.DecisionDefinitionModel, error)
	List(ctx context.Context) ([]models.DecisionDefinitionModel, error)
	ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.DecisionDefinitionModel, error)

	// ListByProjectPaged returns one page of a project's decisions, for the
	// same reason definitions have one.
	ListByProjectPaged(ctx context.Context, projectID uuid.UUID, p Pagination) (Page[models.DecisionDefinitionModel], error)

	// NextVersion proposes the version a new deployment of key should claim,
	// counted over the rows the (project_id, key, version) unique index covers.
	// It is a proposal: concurrent callers get the same number and the index
	// decides which one keeps it.
	NextVersion(ctx context.Context, projectID uuid.UUID, key string) (int, error)

	Create(ctx context.Context, definition models.DecisionDefinitionModel) error
	Update(ctx context.Context, id uuid.UUID, definition models.DecisionDefinitionModel) error
	Delete(ctx context.Context, id uuid.UUID) error
}
