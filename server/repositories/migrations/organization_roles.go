package migrations

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// OrganizationRolesMigration is the version under which a membership came to
// carry the roles its account holds in that organization.
//
// 28 and 29 belong to the audit write order and the notification indexes,
// which were numbered while this was written: versions are identities, and a
// number taken twice stops the server booting.
const OrganizationRolesMigration = 30

// organizationRolesLockWait is the longest migration 30 waits for
// user_organizations.
//
// The ALTER TABLE needs the table to itself for as long as the catalogue takes,
// and while it waits for a reader to finish, PostgreSQL queues every later
// reader of the table behind it. Signing in reads the account's memberships,
// so while a canary runs the release's migrations beside the stable pods, one
// long transaction that had read the table would stop every sign-in for as
// long as it stayed open. The same bound as migration 28's, for the same
// reason.
const organizationRolesLockWait = "2s"

// organizationRoles is migration 30.
//
// Roles were held on the account, so every one of them acted in every
// organization the account belonged to, and an organization's administrator
// granting one granted it on the whole installation. A membership now holds the
// roles its account has in that organization alone.
//
// Nothing is moved. Every existing membership holds an empty list afterwards,
// so each account keeps exactly the access its roles gave it until somebody
// grants one in an organization. Adding a column with a constant default is a
// change to the catalogue in PostgreSQL 11 and later, not a rewrite of the
// table, so the lock is held for no longer than that takes — and it is waited
// for no longer than organizationRolesLockWait.
//
// PostgreSQL only, as 21 to 28. And only where the table exists: a fresh
// installation runs the schema migrations before storm creates
// user_organizations — from the model, with the column already on it.
func organizationRoles() Migration {
	return Migration{
		Version: OrganizationRolesMigration,
		Name:    "an account's roles can be granted in one organization",
		// So the bound on the wait ends with the migration: SET LOCAL.
		Transactional: true,
		Run:           addMembershipRolesWithoutQueueingSignIns,
	}
}

// addMembershipRolesWithoutQueueingSignIns adds user_organizations.roles,
// waiting no longer than organizationRolesLockWait for the table. SET LOCAL:
// the limit ends with the migration's transaction, so the connection goes back
// to the pool as it came.
func addMembershipRolesWithoutQueueingSignIns(ctx context.Context, tx *gorm.DB) error {
	if tx.Name() != "postgres" || !tx.WithContext(ctx).Migrator().HasTable("user_organizations") {
		return nil
	}
	if err := tx.WithContext(ctx).Exec(fmt.Sprintf("SET LOCAL lock_timeout = '%s'", organizationRolesLockWait)).Error; err != nil {
		return fmt.Errorf("bound the wait for user_organizations: %w", err)
	}
	err := tx.WithContext(ctx).Exec(
		`ALTER TABLE user_organizations ADD COLUMN IF NOT EXISTS roles jsonb NOT NULL DEFAULT '[]'::jsonb`,
	).Error
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == postgresLockNotAvailable {
		return fmt.Errorf("user_organizations was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every sign-in behind it, and will finish when started again once that ends: %w",
			organizationRolesLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("give memberships roles of their own: %w", err)
	}
	return nil
}
