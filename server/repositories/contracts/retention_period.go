package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// RetentionPeriodRepository keeps the retention periods administrators give
// processes. A change is a new row and nothing is ever rewritten, so the
// period a process has is its newest row and the rows before it are its
// history.
type RetentionPeriodRepository interface {
	// Set records a change to a process's period. The project must be the
	// caller's and have a version of that process that was not deleted.
	Set(ctx context.Context, period entities.RetentionPeriod) (entities.RetentionPeriod, error)
	// Current lists each process of a project that has a period, with the
	// period it has now, by key. A process returned to the installation's
	// setting is listed with no days.
	Current(ctx context.Context, projectID uuid.UUID) ([]entities.RetentionPeriod, error)
	// History lists the changes to one process's period, newest first.
	History(ctx context.Context, projectID uuid.UUID, processKey string, limit int64) ([]entities.RetentionPeriod, error)
}
