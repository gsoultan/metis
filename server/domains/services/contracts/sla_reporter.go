package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// SLAReporter queries SLA compliance data for process instances.
// Results can be displayed in the SLA dashboard or exported as CSV.
type SLAReporter interface {
	// ListBreachedSLAs returns all SLA entries that have exceeded their due date
	// for the given project, ordered by breach duration descending.
	ListBreachedSLAs(ctx context.Context, projectID uuid.UUID) ([]entities.SLAEntry, error)

	// GetInstanceSLA returns SLA compliance details for every active node
	// of the given process instance.
	GetInstanceSLA(ctx context.Context, instanceID uuid.UUID) ([]entities.SLAEntry, error)
}
