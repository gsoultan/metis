package migrations_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// A membership — the row that puts an account in an organization — now carries
// the roles the account holds in that organization alone. A fresh installation
// gets the column from the storm model, which creates user_organizations after
// the schema migrations have run; an installation that predates it has the table
// without the column, and that is the only place migration 30 does anything.
//
// Nothing is moved into it. A membership that existed before holds no role of
// its own afterwards, so every account keeps exactly the access its global roles
// gave it until an administrator grants one in an organization.
func TestMigration30GivesEveryMembershipRolesOfItsOwn(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	migrate := func() {
		t.Helper()
		if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
			t.Fatalf("run the migrations: %v", err)
		}
	}
	migrate()
	// A fresh install has the column from the model.
	assertMembershipRolesColumn(t, db)

	// user_organizations as an upgrading installation has it: no roles, and a
	// membership already in it.
	organization := insertOrganization(t, db, "Acme")
	account := insertAccount(t, db, "member-before-the-upgrade", "", "")
	if err := db.WithContext(ctx).Exec(`ALTER TABLE user_organizations DROP COLUMN IF EXISTS roles`).Error; err != nil {
		t.Fatalf("restore the old schema: %v", err)
	}
	if err := db.WithContext(ctx).Exec(
		`DELETE FROM schema_migrations WHERE version = ?`, migrations.OrganizationRolesMigration).Error; err != nil {
		t.Fatalf("forget that the migration ran: %v", err)
	}
	addMembership(t, db, account, organization)

	migrate()
	migrate() // and a second run finds nothing left to do

	assertMembershipRolesColumn(t, db)
	if got := membershipRoles(t, db, account, organization); got != "[]" {
		t.Fatalf("a membership that predates the migration holds %s, want no role of its own", got)
	}

	// A membership added as the repository adds one — naming no roles — holds
	// none either, rather than failing on a column it does not mention.
	later := insertAccount(t, db, "member-after-the-upgrade", "", "")
	addMembership(t, db, later, organization)
	if got := membershipRoles(t, db, later, organization); got != "[]" {
		t.Fatalf("a membership added without roles holds %s, want none", got)
	}
}

// assertMembershipRolesColumn checks the column is a JSON list that is never
// NULL: the reader decodes it without a null check, as every column the storm
// model declares non-null.
func assertMembershipRolesColumn(t *testing.T, db *gorm.DB) {
	t.Helper()
	var column struct {
		DataType      string `gorm:"column:data_type"`
		IsNullable    string `gorm:"column:is_nullable"`
		ColumnDefault string `gorm:"column:column_default"`
	}
	if err := db.WithContext(t.Context()).Raw(`
		SELECT data_type, is_nullable, coalesce(column_default, '') AS column_default
		  FROM information_schema.columns
		 WHERE table_schema = current_schema() AND table_name = 'user_organizations' AND column_name = 'roles'`).
		Scan(&column).Error; err != nil {
		t.Fatalf("read user_organizations.roles: %v", err)
	}
	if column.DataType != "jsonb" {
		t.Fatalf("user_organizations.roles is %q, want jsonb", column.DataType)
	}
	if column.IsNullable != "NO" {
		t.Errorf("user_organizations.roles accepts NULL, which the reader cannot decode")
	}
	if !strings.Contains(column.ColumnDefault, "'[]'") {
		t.Errorf("user_organizations.roles defaults to %q, want an empty list", column.ColumnDefault)
	}
}

func insertOrganization(t *testing.T, db *gorm.DB, name string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	now := time.Now()
	if err := db.WithContext(t.Context()).Exec(
		`INSERT INTO organizations (id, created_at, updated_at, name) VALUES (?, ?, ?, ?)`,
		id, now, now, name).Error; err != nil {
		t.Fatalf("insert organization %s: %v", name, err)
	}
	return id
}

func addMembership(t *testing.T, db *gorm.DB, account, organization uuid.UUID) {
	t.Helper()
	if err := db.WithContext(t.Context()).Exec(
		`INSERT INTO user_organizations (user_id, organization_id) VALUES (?, ?)`,
		account, organization).Error; err != nil {
		t.Fatalf("add the membership: %v", err)
	}
}

func membershipRoles(t *testing.T, db *gorm.DB, account, organization uuid.UUID) string {
	t.Helper()
	var roles string
	if err := db.WithContext(t.Context()).Raw(
		`SELECT roles::text FROM user_organizations WHERE user_id = ? AND organization_id = ?`,
		account, organization).Scan(&roles).Error; err != nil {
		t.Fatalf("read the membership's roles: %v", err)
	}
	return roles
}
