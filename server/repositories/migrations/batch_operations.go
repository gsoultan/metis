package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// BatchOperationsMigration is the version under which an act could be asked
// for on many instances at once. 44 is the jobs' organization.
const BatchOperationsMigration = 45

// batchOperationsDDL is frozen text: the tables migration 45 made, which must
// not change when the model does. tests/migrations compares it with the storm
// model and the model is the arbiter.
//
// An operation references its project, so a project with operations cannot be
// deleted out from under their record; an item references its operation. No
// reference to process instances, by the ledger's rule
// (instanceDeviationsDDL): recording what was done to an instance must not
// wait on a row the engine holds. The indexes are plain builds: the tables
// have just been created and are read by nothing yet.
var batchOperationsDDL = []string{
	`CREATE TABLE IF NOT EXISTS batch_operations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    project_id uuid NOT NULL,
    organization_id uuid NOT NULL,
    kind varchar(16) NOT NULL,
    reason text NOT NULL,
    filter_status varchar(32) NOT NULL,
    filter_definition_id uuid,
    filter_business_key varchar(255) NOT NULL,
    filter_needs_attention bool NOT NULL,
    requested_by varchar(255) NOT NULL,
    requested_by_id uuid NOT NULL,
    status varchar(16) NOT NULL,
    total int8 NOT NULL,
    succeeded int8 NOT NULL,
    failed int8 NOT NULL,
    skipped int8 NOT NULL,
    lease_expires timestamptz,
    finished_at timestamptz,
    stopped_by varchar(255) NOT NULL,
    stop_reason text NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT fk_batch_operations_project_id FOREIGN KEY (project_id) REFERENCES projects (id)
)`,
	`CREATE INDEX IF NOT EXISTS ix_batch_operations_project ON batch_operations (project_id, created_at, id)`,
	`CREATE INDEX IF NOT EXISTS ix_batch_operations_running ON batch_operations (created_at) WHERE ((status)::text = 'running'::text)`,
	`CREATE TABLE IF NOT EXISTS batch_operation_items (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    batch_operation_id uuid NOT NULL,
    instance_id uuid NOT NULL,
    status varchar(16) NOT NULL,
    detail varchar(1024) NOT NULL,
    settled_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT fk_batch_operation_items_batch_operation_id FOREIGN KEY (batch_operation_id) REFERENCES batch_operations (id)
)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ux_batch_operation_items_instance ON batch_operation_items (batch_operation_id, instance_id)`,
	`CREATE INDEX IF NOT EXISTS ix_batch_operation_items_status ON batch_operation_items (batch_operation_id, status, instance_id)`,
}

// batchOperations is migration 45.
//
// Retrying or cancelling the instances a filter matched meant one request per
// instance, made by whatever script somebody wrote, with nothing kept of
// which went through. An operation is now a row with an item per instance,
// worked through in the background and each item's result kept.
//
// One transaction, waited for no longer than deviationRequestsLockWait, for
// the reason migration 37 gives: the foreign key takes a brief lock on
// projects. PostgreSQL only, as 21 to 44.
func batchOperations() Migration {
	return Migration{
		Version: BatchOperationsMigration,
		Name:    "an act can be asked for on many instances at once",
		Run:     addBatchOperations,
	}
}

func addBatchOperations(ctx context.Context, db *gorm.DB) error {
	if db.Name() != "postgres" {
		return nil
	}
	err := inBoundedTransaction(ctx, db, batchOperationsDDL...)
	if lockNotAvailable(err) {
		return fmt.Errorf("projects was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every writer of it behind it, and will finish when started again once that ends: %w",
			deviationRequestsLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("create the batch operations: %w", err)
	}
	return nil
}
