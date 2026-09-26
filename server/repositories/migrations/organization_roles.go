package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// OrganizationRolesMigration is the version under which a membership came to
// carry the roles its account holds in that organization.
//
// 28 and 29 are left alone: versions are identities, and a number taken twice
// stops the server booting, while a gap costs nothing.
const OrganizationRolesMigration = 30

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
// table, so the lock is held for no longer than that takes.
//
// PostgreSQL only, as 21 to 27. And only where the table exists: a fresh
// installation runs the schema migrations before storm creates
// user_organizations — from the model, with the column already on it.
func organizationRoles() Migration {
	return Migration{
		Version: OrganizationRolesMigration,
		Name:    "an account's roles can be granted in one organization",
		Run: func(ctx context.Context, db *gorm.DB) error {
			if db.Name() != "postgres" || !db.WithContext(ctx).Migrator().HasTable("user_organizations") {
				return nil
			}
			if err := db.WithContext(ctx).Exec(
				`ALTER TABLE user_organizations ADD COLUMN IF NOT EXISTS roles jsonb NOT NULL DEFAULT '[]'::jsonb`,
			).Error; err != nil {
				return fmt.Errorf("give memberships roles of their own: %w", err)
			}
			return nil
		},
	}
}
