package migrations

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// JobOrganizationMigration is the version under which a job came to say whose
// work it is. 41 is the external task workers'; 40 and 42 are left to the
// branches that claimed them while this one was open.
const JobOrganizationMigration = 44

// jobOrganizationLockWait is the longest migration 44 waits for jobs.
//
// Adding a nullable column with no default changes the catalogue and not the
// table, but it needs the table to itself for that moment, and every worker
// claims from this table several times a second. The same bound as migration
// 39, for the same reason.
const jobOrganizationLockWait = "2s"

// jobOrganization is migration 44.
//
// The claim took the oldest due jobs whoever they belonged to, so one
// organization that queued ten thousand timers at once had every worker until
// they had all run, and every other organization's work waited behind them. A
// job now carries its organization, and the claim takes each organization's
// oldest in turn.
//
// Two steps, each safe to repeat: the column, waited for no longer than
// jobOrganizationLockWait, and the index the claim reads through, built
// CONCURRENTLY, as 39's. Jobs made before it carry no organization and share
// one turn between them; nothing fills them in, because they are run and
// gone soon enough, and a backfill would write every row of the busiest table.
// Not Transactional, because of the second step. PostgreSQL only, as 21 to
// 41, and only where the table exists.
func jobOrganization() Migration {
	return Migration{
		Version: JobOrganizationMigration,
		Name:    "a job carries its organization, so the claim can share workers fairly",
		Run:     addJobOrganization,
	}
}

func addJobOrganization(ctx context.Context, db *gorm.DB) error {
	if db.Name() != "postgres" || !db.WithContext(ctx).Migrator().HasTable("jobs") {
		return nil
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(fmt.Sprintf("SET LOCAL lock_timeout = '%s'", jobOrganizationLockWait)).Error; err != nil {
			return fmt.Errorf("bound the wait for jobs: %w", err)
		}
		return tx.Exec(`ALTER TABLE jobs ADD COLUMN IF NOT EXISTS organization_id uuid`).Error
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == postgresLockNotAvailable {
		return fmt.Errorf("jobs was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every worker's claim behind it, and will finish when started again once that ends: %w",
			jobOrganizationLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("give jobs an organization: %w", err)
	}
	return createIndexConcurrently(ctx, db, "jobs", "ix_jobs_claim_by_organization", "status, organization_id, next_run_at")
}
