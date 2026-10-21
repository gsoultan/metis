package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// ExternalTaskWorkersMigration is the version under which the engine began
// keeping the workers that fetch its external tasks. 39 is the business key's.
const ExternalTaskWorkersMigration = 40

// externalTaskWorkersDDL is frozen text: the table migration 40 made, which
// must not change when the model does. tests/migrations compares it with the
// storm model and the model is the arbiter.
//
// Plain builds: the table has just been created and is read by nothing yet.
var externalTaskWorkersDDL = []string{
	`CREATE TABLE IF NOT EXISTS external_task_workers (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    organization_id uuid NOT NULL,
    topic varchar(255) NOT NULL,
    worker_id varchar(255) NOT NULL,
    version varchar(64),
    last_seen_at timestamptz NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT fk_external_task_workers_organization_id FOREIGN KEY (organization_id) REFERENCES organizations (id)
)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ux_external_task_workers_worker ON external_task_workers (organization_id, topic, worker_id)`,
	`CREATE INDEX IF NOT EXISTS ix_external_task_workers_last_seen_at ON external_task_workers (last_seen_at)`,
}

// externalTaskWorkers is migration 40.
//
// A topic whose tasks waited with no worker fetching them looked, from the
// tasks, exactly like a topic whose workers were busy: nothing recorded that a
// worker had ever asked. Each fetch now says which worker asked and when, so
// an operator can see who serves a topic and be told when nobody does.
//
// One transaction, waited for no longer than deviationRequestsLockWait: the
// foreign key takes a brief lock on organizations. PostgreSQL only, as 21 to
// 39.
func externalTaskWorkers() Migration {
	return Migration{
		Version: ExternalTaskWorkersMigration,
		Name:    "the engine keeps the workers that fetch its external tasks",
		Run:     addExternalTaskWorkers,
	}
}

func addExternalTaskWorkers(ctx context.Context, db *gorm.DB) error {
	if db.Name() != "postgres" {
		return nil
	}
	err := inBoundedTransaction(ctx, db, externalTaskWorkersDDL...)
	if lockNotAvailable(err) {
		return fmt.Errorf("organizations was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every writer of it behind it, and will finish when started again once that ends: %w",
			deviationRequestsLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("create the external task workers: %w", err)
	}
	return nil
}
