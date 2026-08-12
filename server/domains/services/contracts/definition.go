package contracts

import (
	"context"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

// DefinitionService defines the process definition operations.
type DefinitionService interface {
	CreateDefinition(ctx context.Context, def *entities.ProcessDefinition) (uuid.UUID, error)
	ListDefinitions(ctx context.Context, projectID uuid.UUID) ([]*entities.ProcessDefinition, error)
	GetDefinition(ctx context.Context, id uuid.UUID) (*entities.ProcessDefinition, error)
	GetDefinitionByKey(ctx context.Context, key string) (*entities.ProcessDefinition, error)
	DeleteDefinition(ctx context.Context, id uuid.UUID) error
	ExportDefinition(ctx context.Context, id uuid.UUID) ([]byte, error)
	ImportDefinition(ctx context.Context, xml []byte) (uuid.UUID, error)
}
