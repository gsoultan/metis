package migrations

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// InstanceDeviationsMigration is the version under which an instance came to
// keep a ledger of what was done to it outside its process. 31 and 32 belong to
// the task's iteration and its delegation; versions are identities, and a
// number taken twice stops the server booting.
const InstanceDeviationsMigration = 33

// instanceDeviationsLockWait is the longest migration 33 waits for projects and
// process_definitions.
//
// The new table's foreign keys take a brief lock on each table they reference,
// and while CREATE TABLE waits for a long reader of one of them, PostgreSQL
// queues every later writer of that table behind it. The same bound as
// migrations 28, 30 and 32, for the same reason.
const instanceDeviationsLockWait = "2s"

// instanceDeviationsDDL is frozen text: what migration 33 did, which must not
// change when the model does. tests/migrations compares it with the storm model
// and the model is the arbiter.
//
// There is no foreign key to process_instances or to tasks, on purpose. A
// hand-over holds the task's row and inserts a ledger row; a completion holds
// the instance and then waits for the task's row. A reference from the ledger
// to either would make the hand-over's insert wait for a lock the completion
// holds while the completion waits for the hand-over: a deadlock. The same rule
// as audit_logs and notifications, which task.go states.
var instanceDeviationsDDL = []string{
	`CREATE TABLE IF NOT EXISTS instance_deviations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    project_id uuid NOT NULL,
    instance_id uuid NOT NULL,
    definition_id uuid,
    kind varchar(32) NOT NULL,
    scope varchar(16) NOT NULL,
    origin varchar(16) NOT NULL,
    status varchar(32) NOT NULL,
    node_id varchar(255),
    node_name varchar(255),
    task_id uuid,
    iteration_id varchar(191),
    actor varchar(255) NOT NULL,
    actor_id uuid,
    reason text,
    before jsonb NOT NULL,
    after jsonb NOT NULL,
    details jsonb NOT NULL,
    run_id uuid NOT NULL,
    audit_entry_id uuid,
    visit_key varchar(64),
    request_id uuid,
    approved_by varchar(255),
    approved_by_id uuid,
    decided_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT fk_instance_deviations_project_id FOREIGN KEY (project_id) REFERENCES projects (id),
    CONSTRAINT fk_instance_deviations_definition_id FOREIGN KEY (definition_id) REFERENCES process_definitions (id) ON DELETE SET NULL
)`,
	`CREATE INDEX IF NOT EXISTS ix_instance_deviations_project_id ON instance_deviations (project_id)`,
	`CREATE INDEX IF NOT EXISTS ix_instance_deviations_definition_id ON instance_deviations (definition_id)`,
	`CREATE INDEX IF NOT EXISTS ix_instance_deviations_instance ON instance_deviations (instance_id, created_at, id)`,
	`CREATE INDEX IF NOT EXISTS ix_instance_deviations_run ON instance_deviations (run_id)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ux_instance_deviations_visit ON instance_deviations (instance_id, visit_key)`,
}

// instanceDeviations is migration 33.
//
// Before it, what was done to a running instance outside its process — a task
// waived, an instance cancelled, work handed to somebody the model did not name —
// left a line in the audit trail and nothing an auditor could ask for by
// instance. The ledger is that record: one row per act, with what it changed.
//
// A new table, so nothing is moved and nothing is backfilled; history before
// this release stays in the audit trail where it was. Every statement is
// IF NOT EXISTS, so a run that stops part-way finishes when started again.
// Transactional, so the bound on the wait ends with the migration: SET LOCAL.
// PostgreSQL only, as 21 to 32.
func instanceDeviations() Migration {
	return Migration{
		Version:       InstanceDeviationsMigration,
		Name:          "an instance keeps a ledger of what was done to it outside its process",
		Transactional: true,
		Run:           createInstanceDeviations,
	}
}

func createInstanceDeviations(ctx context.Context, tx *gorm.DB) error {
	if tx.Name() != "postgres" {
		return nil
	}
	if err := tx.WithContext(ctx).Exec(fmt.Sprintf("SET LOCAL lock_timeout = '%s'", instanceDeviationsLockWait)).Error; err != nil {
		return fmt.Errorf("bound the wait for projects and process_definitions: %w", err)
	}
	for _, stmt := range instanceDeviationsDDL {
		err := tx.WithContext(ctx).Exec(stmt).Error
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == postgresLockNotAvailable {
			return fmt.Errorf("projects or process_definitions was held for more than %s by a long query or transaction; "+
				"the upgrade stopped rather than hold every writer of either behind it, and will finish when started again once that ends: %w",
				instanceDeviationsLockWait, err)
		}
		if err != nil {
			return fmt.Errorf("create the instance deviation ledger: %w", err)
		}
	}
	return nil
}
