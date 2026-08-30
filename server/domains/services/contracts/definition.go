package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// DefinitionService defines the process definition operations.
type DefinitionService interface {
	CreateDefinition(ctx context.Context, def *entities.ProcessDefinition) (uuid.UUID, error)
	ListDefinitions(ctx context.Context, projectID uuid.UUID) ([]*entities.ProcessDefinition, error)

	// ListDefinitionsPaged returns one page of a project's definitions.
	ListDefinitionsPaged(ctx context.Context, projectID uuid.UUID, page repocontracts.Pagination) (repocontracts.Page[*entities.ProcessDefinition], error)
	GetDefinition(ctx context.Context, id uuid.UUID) (*entities.ProcessDefinition, error)
	GetDefinitionByKey(ctx context.Context, key string) (*entities.ProcessDefinition, error)
	DeleteDefinition(ctx context.Context, id uuid.UUID) error
	ExportDefinition(ctx context.Context, id uuid.UUID) ([]byte, error)
	ImportDefinition(ctx context.Context, projectID uuid.UUID, xml []byte) (uuid.UUID, error)

	// ListJavaScriptConditions reports every stored `js:` condition the caller
	// can see — the worklist for the javascript-conditions flag, which refuses
	// them by default.
	ListJavaScriptConditions(ctx context.Context) ([]entities.JavaScriptConditionUsage, error)
}
