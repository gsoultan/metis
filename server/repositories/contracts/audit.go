package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/models"
)

type AuditRepository interface {
	Create(ctx context.Context, entry models.AuditModel) error
	ListByInstance(ctx context.Context, instanceID uuid.UUID) ([]models.AuditModel, error)
	// LatestByInstance returns limit entries of an instance's history counted
	// back from its newest, after skipping the newest offset, oldest first; and
	// how many entries the instance has in all.
	LatestByInstance(ctx context.Context, instanceID uuid.UUID, limit, offset int64) ([]models.AuditModel, int64, error)
	// NodeVisits counts the entries of eventType for each node of an
	// instance, in the order each node was first reached.
	NodeVisits(ctx context.Context, instanceID uuid.UUID, eventType string) ([]NodeVisit, error)
	// ListByProject returns at most limit of a project's entries, oldest
	// first. The limit is the caller's, because a project's whole trail is
	// more than any one read should hold.
	ListByProject(ctx context.Context, projectID uuid.UUID, limit int64) ([]models.AuditModel, error)
}

// NodeVisit is how often an instance reached one node.
type NodeVisit struct {
	NodeID string
	Visits int
}
