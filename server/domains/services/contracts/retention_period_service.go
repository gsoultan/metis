package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// RetentionPeriodService gives processes retention periods of their own: how
// long, for every version of one process, its finished instances are kept
// before the retention sweep removes them.
//
// Setting a period is an administrator's of the project's organization, with
// a reason; every change is kept, with who made it and why. Reading is open
// to whoever may read the project.
type RetentionPeriodService interface {
	// SetRetentionPeriod gives a process a period of days, or with nil days
	// returns it to the installation's setting.
	SetRetentionPeriod(ctx context.Context, projectID uuid.UUID, processKey string, days *int, reason string) (entities.RetentionPeriod, error)
	// RetentionPeriods lists the processes of a project that were ever given
	// a period, each with the one it has now.
	RetentionPeriods(ctx context.Context, projectID uuid.UUID) ([]entities.RetentionPeriod, error)
	// RetentionPeriodHistory lists the changes to one process's period,
	// newest first.
	RetentionPeriodHistory(ctx context.Context, projectID uuid.UUID, processKey string) ([]entities.RetentionPeriod, error)
}
