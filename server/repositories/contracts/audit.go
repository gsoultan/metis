package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

type AuditRepository interface {
	Create(ctx context.Context, entry models.AuditModel) error
	ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.AuditModel, error)
	// ListByProject returns at most limit of a project's entries, oldest
	// first. The limit is the caller's, because a project's whole trail is
	// more than any one read should hold.
	ListByProject(ctx context.Context, projectID uuid.UUID, limit int64) ([]models.AuditModel, error)
}
