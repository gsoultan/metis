package migrations

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// BusinessKeyMigration is the version under which an instance came to carry
// the caller's name for its case. 38 is the retention periods'.
const BusinessKeyMigration = 39

// businessKeyLockWait is the longest migration 39 waits for process_instances.
//
// Adding a nullable column with no default changes the catalogue and not the
// table, but it needs the table to itself for that moment, and while it waits
// for a reader PostgreSQL queues every later reader and writer behind it.
// Every step of every instance reads or writes this table. The same bound as
// migration 32, for the same reason.
const businessKeyLockWait = "2s"

// businessKey is migration 39.
//
// A caller who started an instance for an order, a claim or a ticket had only
// the instance's id to find it by again, and an operator asked about "order
// 4411" had to read variables instance by instance. An instance now carries
// that name, given when it is started, and the instance list finds a
// project's instances by it.
//
// Two steps, each safe to repeat: the column, waited for no longer than
// businessKeyLockWait, and the index the list reads through, built
// CONCURRENTLY, as 32's: a plain build would lock the table against every
// step of every instance for as long as it took. Not Transactional, because
// of the second step. PostgreSQL only, as 21 to 38, and only where the table
// exists: anywhere else the baseline has just built it from the current
// model.
func businessKey() Migration {
	return Migration{
		Version: BusinessKeyMigration,
		Name:    "an instance carries its caller's business key",
		Run:     addBusinessKey,
	}
}

func addBusinessKey(ctx context.Context, db *gorm.DB) error {
	if db.Name() != "postgres" || !db.WithContext(ctx).Migrator().HasTable("process_instances") {
		return nil
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(fmt.Sprintf("SET LOCAL lock_timeout = '%s'", businessKeyLockWait)).Error; err != nil {
			return fmt.Errorf("bound the wait for process_instances: %w", err)
		}
		return tx.Exec(`ALTER TABLE process_instances ADD COLUMN IF NOT EXISTS business_key varchar(255)`).Error
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == postgresLockNotAvailable {
		return fmt.Errorf("process_instances was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every running instance behind it, and will finish when started again once that ends: %w",
			businessKeyLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("give instances a business key: %w", err)
	}
	return createIndexConcurrently(ctx, db, "process_instances", "ix_process_instances_business_key", "project_id, business_key")
}
