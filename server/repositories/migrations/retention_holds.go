package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// RetentionHoldsMigration is the version under which an instance could be
// kept out of the finished-instance sweep. 36 is the task list's index;
// versions are identities, and a number taken twice stops the server booting.
const RetentionHoldsMigration = 37

// retentionHoldsDDL is frozen text: the table migration 37 made, which must
// not change when the model does. tests/migrations compares it with the storm
// model and the model is the arbiter.
//
// There is no foreign key to process_instances, by the ledger's rule
// (instanceDeviationsDDL): placing a hold must not wait on a row the engine
// holds. The reference to projects is plain, so a project whose instances are
// held cannot be deleted while they are. The indexes are plain builds: the
// table has just been created and is read by nothing yet.
var retentionHoldsDDL = []string{
	`CREATE TABLE IF NOT EXISTS retention_holds (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    project_id uuid NOT NULL,
    instance_id uuid NOT NULL,
    reason text NOT NULL,
    placed_by varchar(255) NOT NULL,
    placed_by_id uuid NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT fk_retention_holds_project_id FOREIGN KEY (project_id) REFERENCES projects (id)
)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ux_retention_holds_instance ON retention_holds (instance_id)`,
	`CREATE INDEX IF NOT EXISTS ix_retention_holds_project ON retention_holds (project_id, created_at, id)`,
}

// retentionHolds is migration 37.
//
// The finished-instance sweep removed every completed or cancelled instance
// past its period that nothing running still needed, and an administrator had
// no way to say that one of them must stay: a case under a legal hold, or one
// an auditor asked to be kept. A hold is now a row of its own, and the sweep
// leaves alone every instance a hold names, with the whole case of calls it
// is part of.
//
// One transaction, waited for no longer than deviationRequestsLockWait: the
// foreign key takes a brief lock on projects, which a writer's open
// transaction holds off, and while it waits PostgreSQL queues every later
// writer of projects behind it. PostgreSQL only, as 21 to 36.
func retentionHolds() Migration {
	return Migration{
		Version: RetentionHoldsMigration,
		Name:    "an instance can be kept out of the retention sweep",
		Run:     addRetentionHolds,
	}
}

func addRetentionHolds(ctx context.Context, db *gorm.DB) error {
	if db.Name() != "postgres" {
		return nil
	}
	err := inBoundedTransaction(ctx, db, retentionHoldsDDL...)
	if lockNotAvailable(err) {
		return fmt.Errorf("projects was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every writer of it behind it, and will finish when started again once that ends: %w",
			deviationRequestsLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("create the retention holds: %w", err)
	}
	return nil
}
