package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

type FormService interface {
	CreateForm(ctx context.Context, projectID uuid.UUID, key, name string, schema map[string]any) (entities.Form, error)
	GetForm(ctx context.Context, id uuid.UUID) (entities.Form, error)
	GetFormByKey(ctx context.Context, projectID uuid.UUID, key string) (entities.Form, error)
	ListForms(ctx context.Context, projectID uuid.UUID) ([]entities.Form, error)
	DeleteForm(ctx context.Context, id uuid.UUID) error
}
