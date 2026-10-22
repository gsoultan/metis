package migrations

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// AuditActorMigration is the version under which an audit entry came to name,
// in a column of its own, the person who acted. 45 is the batch operations.
const AuditActorMigration = 46

// auditActorLockWait is the longest migration 46 waits for audit_logs, for
// the reason auditWriteOrderLockWait gives: every step of every running
// process writes to the table, and an ALTER waiting behind a long read queues
// all of them behind it.
const auditActorLockWait = "2s"

// auditActor is migration 46.
//
// Who suspended, waived, migrated or reassigned something was written down
// only inside an entry's data, which is sealed: an administrator could read it
// on one instance's history and nowhere else, because no query can look
// inside it. The entry now names the actor in a plain column as well, and the
// audit log reads the entries that have one, newest first, across the
// organization.
//
// A nullable column with no default changes the catalogue and no row, waited
// for no longer than auditActorLockWait; then two partial indexes, built
// CONCURRENTLY, over only the entries that name somebody: one for the whole
// log in order, one for one person's. Partial because nearly every entry is
// the engine's own, and the log never reads those.
//
// Entries written before this migration are left as they are. Their actor is
// in sealed data, and filling the column would mean decrypting and rewriting
// every audit row an installation has ever written. They stay on their
// instance's history, as they always were. Not Transactional, because of the
// indexes. PostgreSQL only, as 21 onwards: anywhere else the baseline has just
// built the table from the current model.
func auditActor() Migration {
	return Migration{
		Version: AuditActorMigration,
		Name:    "an audit entry names who acted, so the operations can be listed",
		Run:     addAuditActor,
	}
}

func addAuditActor(ctx context.Context, db *gorm.DB) error {
	if db.Name() != "postgres" || !db.WithContext(ctx).Migrator().HasTable("audit_logs") {
		return nil
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(fmt.Sprintf("SET LOCAL lock_timeout = '%s'", auditActorLockWait)).Error; err != nil {
			return fmt.Errorf("bound the wait for audit_logs: %w", err)
		}
		return tx.Exec(`ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS actor varchar(255)`).Error
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == postgresLockNotAvailable {
		return fmt.Errorf("audit_logs was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every running process behind it, and will finish when started again once that ends: %w",
			auditActorLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("give audit entries an actor: %w", err)
	}
	if err := createPartialIndexConcurrently(ctx, db, "audit_logs", "ix_audit_logs_operations",
		"created_at, seq", "(actor IS NOT NULL)"); err != nil {
		return err
	}
	return createPartialIndexConcurrently(ctx, db, "audit_logs", "ix_audit_logs_operations_by_actor",
		"actor, created_at, seq", "(actor IS NOT NULL)")
}
