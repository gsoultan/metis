package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// RetentionPeriodsMigration is the version under which a process could be
// given a retention period of its own. 37 is the retention holds'.
const RetentionPeriodsMigration = 38

// retentionPeriodsDDL is frozen text: the table migration 38 made, which must
// not change when the model does. tests/migrations compares it with the storm
// model and the model is the arbiter.
//
// The reference to projects is plain, as the holds' is. A period names a
// process by its key and not by a definition row: it applies to every version
// of the process, including ones not yet deployed. The index is a plain build:
// the table has just been created and is read by nothing yet.
var retentionPeriodsDDL = []string{
	`CREATE TABLE IF NOT EXISTS retention_periods (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    project_id uuid NOT NULL,
    process_key varchar(255) NOT NULL,
    days integer,
    reason text NOT NULL,
    set_by varchar(255) NOT NULL,
    set_by_id uuid NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT fk_retention_periods_project_id FOREIGN KEY (project_id) REFERENCES projects (id)
)`,
	`CREATE INDEX IF NOT EXISTS ix_retention_periods_process ON retention_periods (project_id, process_key, created_at, id)`,
}

// retentionPeriods is migration 38.
//
// The finished-instance sweep kept every process's instances for the one
// period the installation was given, and a process whose records the law
// says to keep for ten years ran beside one whose records are worth a month.
// A period of a process's own is now a row, and the sweep keeps each finished
// instance for its process's period, or the installation's where its process
// has none.
//
// One transaction, waited for no longer than deviationRequestsLockWait, for
// the reason migration 37 gives: the foreign key takes a brief lock on
// projects. PostgreSQL only, as 21 to 37.
func retentionPeriods() Migration {
	return Migration{
		Version: RetentionPeriodsMigration,
		Name:    "a process can have a retention period of its own",
		Run:     addRetentionPeriods,
	}
}

func addRetentionPeriods(ctx context.Context, db *gorm.DB) error {
	if db.Name() != "postgres" {
		return nil
	}
	err := inBoundedTransaction(ctx, db, retentionPeriodsDDL...)
	if lockNotAvailable(err) {
		return fmt.Errorf("projects was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every writer of it behind it, and will finish when started again once that ends: %w",
			deviationRequestsLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("create the retention periods: %w", err)
	}
	return nil
}
