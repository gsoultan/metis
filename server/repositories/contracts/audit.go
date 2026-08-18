package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

type AuditRepository interface {
	Create(ctx context.Context, entry models.AuditModel) error
	ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.AuditModel, error)
	ListByProject(ctx context.Context, projectID uuid.UUID) ([]models.AuditModel, error)
}
