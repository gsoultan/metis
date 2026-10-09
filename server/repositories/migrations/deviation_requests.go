package migrations

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// DeviationRequestsMigration is the version under which a waive, and a
// migration that loosens a rule, came to wait for a second administrator. 33 is
// the ledger; versions are identities, and a number taken twice stops the
// server booting.
const DeviationRequestsMigration = 34

// deviationRequestsLockWait is the longest migration 34 waits for a table or a
// row somebody else holds.
//
// The new table's foreign keys take a brief lock on projects and
// process_definitions, and adding a column needs the ledger to itself for as
// long as the catalogue takes. While either waits for a long reader,
// PostgreSQL queues every later writer of that table behind it — and a
// hand-over and a completion write the ledger. The same bound as migrations
// 28, 30, 32 and 33, for the same reason.
const deviationRequestsLockWait = "2s"

// deviationRequestsBatch is how many ledger rows one transaction of the
// backfill touches: the statement that picks them and the statement that
// updates them. The size migration 32 uses, for its reason: each transaction
// short enough to interleave with work.
const deviationRequestsBatch = 5000

// postgresForeignKeyViolation is foreign_key_violation: a row names a parent
// that is not there.
const postgresForeignKeyViolation = "23503"

// deviationRequestsDDL is frozen text: the table migration 34 made, which must
// not change when the model does. tests/migrations compares it with the storm
// model and the model is the arbiter.
//
// There is no foreign key to process_instances, as on the ledger and by its
// rule (instanceDeviationsDDL): nothing written beside a hand-over or a
// completion references the rows those lock. The indexes are plain builds: the
// table has just been created, holds nothing and is read by nothing yet. There
// is no index on project_id alone: two here start with it.
var deviationRequestsDDL = []string{
	`CREATE TABLE IF NOT EXISTS deviation_requests (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    project_id uuid NOT NULL,
    kind varchar(32) NOT NULL,
    status varchar(32) NOT NULL,
    instance_id uuid,
    source_definition_id uuid,
    target_definition_id uuid,
    requested_by varchar(255) NOT NULL,
    requested_by_id uuid NOT NULL,
    reason text NOT NULL,
    command jsonb NOT NULL,
    plan jsonb NOT NULL,
    fingerprint varchar(64) NOT NULL,
    live_key varchar(64),
    approved_instances jsonb NOT NULL,
    expires_at timestamptz NOT NULL,
    decided_by varchar(255),
    decided_by_id uuid,
    decision_reason text,
    decided_at timestamptz,
    outcome jsonb NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT fk_deviation_requests_project_id FOREIGN KEY (project_id) REFERENCES projects (id),
    CONSTRAINT fk_deviation_requests_source_definition_id FOREIGN KEY (source_definition_id) REFERENCES process_definitions (id) ON DELETE SET NULL,
    CONSTRAINT fk_deviation_requests_target_definition_id FOREIGN KEY (target_definition_id) REFERENCES process_definitions (id) ON DELETE SET NULL
)`,
	`CREATE INDEX IF NOT EXISTS ix_deviation_requests_source_definition_id ON deviation_requests (source_definition_id)`,
	`CREATE INDEX IF NOT EXISTS ix_deviation_requests_target_definition_id ON deviation_requests (target_definition_id)`,
	`CREATE INDEX IF NOT EXISTS ix_deviation_requests_queue ON deviation_requests (project_id, status, created_at, id)`,
	`CREATE INDEX IF NOT EXISTS ix_deviation_requests_sweep ON deviation_requests (status, expires_at)`,
	`CREATE INDEX IF NOT EXISTS ix_deviation_requests_instance ON deviation_requests (instance_id)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ux_deviation_requests_live_key ON deviation_requests (project_id, live_key)`,
}

// ledgerRequestDDL is frozen text: what migration 34 did to the ledger's
// catalogue. A nullable column with no default is not a rewrite of the table,
// and a foreign key added NOT VALID checks no row that is already there — both
// take their lock for as long as the catalogue takes and no longer. ADD
// CONSTRAINT has no IF NOT EXISTS, so the catalogue is asked first.
var ledgerRequestDDL = []string{
	`ALTER TABLE instance_deviations ADD COLUMN IF NOT EXISTS live_visit_key varchar(64)`,
	`DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint c JOIN pg_class t ON t.oid = c.conrelid JOIN pg_namespace n ON n.oid = t.relnamespace
                  WHERE c.conname = 'fk_instance_deviations_request_id' AND t.relname = 'instance_deviations' AND n.nspname = current_schema()) THEN
    ALTER TABLE instance_deviations ADD CONSTRAINT fk_instance_deviations_request_id
      FOREIGN KEY (request_id) REFERENCES deviation_requests (id) NOT VALID;
  END IF;
END $$`,
}

// deviationRequests is migration 34.
//
// A waive, and a migration that skips a step or drops a control, were one
// administrator's call. They now wait as a request a second administrator
// approves, and the ledger's rule of one live row per visit moves off the
// visit key — under which a waive somebody rejected could never be asked for
// again — onto a key a row holds only while it is applied or waiting.
//
// Six steps, each safe to repeat, so a run that stops part-way finishes when
// it is started again:
//
//  1. The requests table and its indexes, in one transaction.
//  2. The ledger's new column and its reference to a request, NOT VALID, in
//     another: changes to the catalogue, waited for no longer than
//     deviationRequestsLockWait. A transaction of its own, so that the wait
//     for the ledger is not spent holding the locks step 1 took on projects
//     and process_definitions: in one transaction, a long reader of the
//     ledger would hold every writer of those two for as long as the wait. A
//     run that stops between the two finds the table there when it is started
//     again, and goes on to the ledger.
//  3. Every row that holds its visit is given its live key. In batches.
//  4. The ledger's three new indexes, built CONCURRENTLY, as 20, 29 and 32: a
//     plain build would lock the ledger against every hand-over and completion
//     for as long as it took.
//  5. 33's unique index on the visit key is dropped, CONCURRENTLY, and only
//     now: until the new unique index is valid, it is what refuses a second
//     live row for a visit.
//  6. The reference is validated. That reads every row, under a lock that
//     stops no writer.
//
// Not Transactional, because of steps 4 and 5; the others take transactions of
// their own so the bound on a wait ends with each. PostgreSQL only, as 21 to
// 33.
//
// A pod of the release before, still serving while this runs, writes ledger
// rows with no live key. The new unique index does not see them; the instance
// lock and the read of the visit's row, which every act makes first, still
// stop a second act on a visit.
func deviationRequests() Migration {
	return Migration{
		Version: DeviationRequestsMigration,
		Name:    "a waive or a rule-loosening migration waits for a second administrator",
		Run:     addDeviationRequests,
	}
}

func addDeviationRequests(ctx context.Context, db *gorm.DB) error {
	if db.Name() != "postgres" {
		return nil
	}
	for _, step := range []func(context.Context, *gorm.DB) error{
		createDeviationRequests,
		giveTheLedgerItsLiveKeyAndRequest,
		FillLiveVisitKeys,
		moveVisitUniquenessToTheLiveKey,
		validateTheLedgersRequests,
	} {
		if err := step(ctx, db); err != nil {
			return err
		}
	}
	return nil
}

// createDeviationRequests creates the requests table, waiting no longer than
// deviationRequestsLockWait for the tables it references.
func createDeviationRequests(ctx context.Context, db *gorm.DB) error {
	err := inBoundedTransaction(ctx, db, deviationRequestsDDL...)
	if lockNotAvailable(err) {
		return fmt.Errorf("projects or process_definitions was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every writer of either behind it, and will finish when started again once that ends: %w",
			deviationRequestsLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("create the deviation requests: %w", err)
	}
	return nil
}

// giveTheLedgerItsLiveKeyAndRequest adds instance_deviations.live_visit_key and
// the reference from request_id, waiting no longer than
// deviationRequestsLockWait for the ledger.
func giveTheLedgerItsLiveKeyAndRequest(ctx context.Context, db *gorm.DB) error {
	err := inBoundedTransaction(ctx, db, ledgerRequestDDL...)
	if lockNotAvailable(err) {
		return fmt.Errorf("instance_deviations was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every hand-over and every completion behind it, and will finish when started again once that ends: %w",
			deviationRequestsLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("give the ledger its live key and its reference to a request: %w", err)
	}
	return nil
}

// FillLiveVisitKeys (exported so the test that races it against a decision can
// run it without the ALTER TABLE that precedes it in the migration, which
// would wait for that decision instead of racing it) gives every ledger row
// that holds its visit — applied, or waiting for approval — its live key. Only
// rows with none are touched, so a row written since is left alone.
//
// Written as migration 32's backfill is. Each batch is a short transaction of
// its own: it picks up to deviationRequestsBatch ids, then updates them with
// the predicates repeated in the outer WHERE. Repeated, because PostgreSQL
// re-checks only the outer condition against a row another transaction changed
// while this one waited for it; with the predicates only on the statement that
// picks, a row rejected meanwhile would be given a key that refuses every
// later request for its visit.
//
// The loop ends when a batch SELECTS no ids, never when it updates none: a
// batch whose rows were all changed by others updates zero rows while matching
// rows may remain. Every id picked either is updated or has been changed so
// that it no longer matches, so the loop converges.
//
// 33's unique index on (instance_id, visit_key) is still in place while this
// runs, so no two rows of an instance can be given one key. lock_timeout
// bounds the wait on a row another transaction holds, so a long transaction
// cannot make a batch sit on the locks it already took; the migration is
// restartable and says so.
func FillLiveVisitKeys(ctx context.Context, db *gorm.DB) error {
	filled, err := GiveLiveRowsTheirKey(ctx, db)
	if lockNotAvailable(err) {
		return fmt.Errorf("a row of instance_deviations was held for more than %s by a long transaction; "+
			"the upgrade stopped rather than wait on it with row locks held, and will finish when started again once that ends: %w",
			deviationRequestsLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("give the ledger's live rows their live key: %w", err)
	}
	log.Info().Int("filled", filled).
		Msg("Ledger rows that hold a visit were given the key that holds it")
	return nil
}

// GiveLiveRowsTheirKey is the work of FillLiveVisitKeys — every batch of it,
// as described there — answering how many rows it filled and saying nothing:
// its errors are as they came, and it logs no line.
//
// It is apart from the migration's step because the migration is not its
// only caller. A pod of the previous release writes a live row with no live
// key, and such a pod runs beside this release during a rolling upgrade and
// after a rollback — after the migration has made its one pass. So the
// server's retention pass calls this on every database, every time: it
// touches only rows with no key, costs one short read when there are none,
// and is safe to run from every replica at once. What to say of the result
// is the caller's.
func GiveLiveRowsTheirKey(ctx context.Context, db *gorm.DB) (int, error) {
	filled := 0
	for {
		var ids []string
		var n int64
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(fmt.Sprintf("SET LOCAL lock_timeout = '%s'", deviationRequestsLockWait)).Error; err != nil {
				return fmt.Errorf("bound the wait for a ledger row: %w", err)
			}
			if err := tx.Raw(`SELECT id::text FROM instance_deviations
				WHERE visit_key IS NOT NULL AND live_visit_key IS NULL AND status IN ('applied', 'pending_approval')
				LIMIT ?`, deviationRequestsBatch).Scan(&ids).Error; err != nil {
				return err
			}
			if len(ids) == 0 {
				return nil
			}
			res := tx.Exec(`
				UPDATE instance_deviations
				   SET live_visit_key = visit_key
				 WHERE id IN ?
				   AND visit_key IS NOT NULL AND live_visit_key IS NULL AND status IN ('applied', 'pending_approval')`, ids)
			n = res.RowsAffected
			return res.Error
		})
		if err != nil {
			return filled, err
		}
		if len(ids) == 0 {
			return filled, nil
		}
		filled += int(n)
	}
}

// moveVisitUniquenessToTheLiveKey builds the ledger's new indexes without
// locking it against writes, and only then drops the unique index migration 33
// made. Each build skips an index that is already valid and rebuilds one an
// earlier attempt left invalid.
func moveVisitUniquenessToTheLiveKey(ctx context.Context, db *gorm.DB) error {
	if err := createUniqueIndexConcurrently(ctx, db, "instance_deviations",
		"ux_instance_deviations_live_visit", "instance_id, live_visit_key"); err != nil {
		return err
	}
	if err := createIndexConcurrently(ctx, db, "instance_deviations",
		"ix_instance_deviations_visit", "instance_id, visit_key"); err != nil {
		return err
	}
	if err := createIndexConcurrently(ctx, db, "instance_deviations",
		"ix_instance_deviations_request_id", "request_id"); err != nil {
		return err
	}
	if err := db.WithContext(ctx).Exec(
		`DROP INDEX CONCURRENTLY IF EXISTS ux_instance_deviations_visit`).Error; err != nil {
		return fmt.Errorf("drop the unique index on the visit key: %w", err)
	}
	return nil
}

// validateTheLedgersRequests checks every ledger row against the reference
// added NOT VALID. The scan holds a lock that stops no writer of the ledger,
// and a constraint already valid validates at once.
//
// No release before this one wrote request_id, so every row passes. One that
// does not was written by hand, and the upgrade stops and says so rather than
// clear or delete anything in a compliance record.
func validateTheLedgersRequests(ctx context.Context, db *gorm.DB) error {
	err := inBoundedTransaction(ctx, db,
		`ALTER TABLE instance_deviations VALIDATE CONSTRAINT fk_instance_deviations_request_id`)
	if lockNotAvailable(err) {
		return fmt.Errorf("instance_deviations was held for more than %s by another change to its schema or a vacuum; "+
			"the upgrade stopped rather than wait behind it, and will finish when started again once that ends: %w",
			deviationRequestsLockWait, err)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == postgresForeignKeyViolation {
		return fmt.Errorf("a row of instance_deviations has a request_id that no row of deviation_requests has; "+
			"no release wrote one before this upgrade, so it was written by hand. The upgrade stopped rather than change a compliance record: "+
			"set request_id to NULL on such rows, and it will finish when started again: %w", err)
	}
	if err != nil {
		return fmt.Errorf("validate the ledger's reference to its request: %w", err)
	}
	return nil
}

// inBoundedTransaction runs the statements in one transaction that waits no
// longer than deviationRequestsLockWait for any lock. SET LOCAL: the limit
// ends with the transaction, so the connection goes back to the pool as it
// came.
func inBoundedTransaction(ctx context.Context, db *gorm.DB, statements ...string) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(fmt.Sprintf("SET LOCAL lock_timeout = '%s'", deviationRequestsLockWait)).Error; err != nil {
			return fmt.Errorf("bound the wait for a lock: %w", err)
		}
		for _, statement := range statements {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// lockNotAvailable reports whether err is PostgreSQL giving up on a lock when
// lock_timeout ran out.
func lockNotAvailable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == postgresLockNotAvailable
}
