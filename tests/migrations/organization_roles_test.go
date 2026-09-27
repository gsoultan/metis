package migrations_test

import (
	"context"
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

// Adding the column needs user_organizations to itself for as long as the
// catalogue takes, and while the ALTER TABLE waits for a reader, PostgreSQL
// queues every later reader of the table behind it. Signing in reads an
// account's memberships, so while a canary runs the release's migrations beside
// the stable pods, one long transaction that had read the table would stop
// every sign-in for as long as it stayed open. The migration gives up after a
// bounded wait instead, says why, and runs when it is started again.
func TestMigration30GivesUpRatherThanHoldEverySignInBehindALongRead(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	schema := migrations.Schema(models.MigrationModels())
	if _, err := migrations.Run(ctx, db, schema); err != nil {
		t.Fatalf("run the migrations: %v", err)
	}
	if err := db.WithContext(ctx).Exec(`ALTER TABLE user_organizations DROP COLUMN IF EXISTS roles`).Error; err != nil {
		t.Fatalf("restore the old schema: %v", err)
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = ?`,
		migrations.OrganizationRolesMigration).Error; err != nil {
		t.Fatalf("rewind the migration record: %v", err)
	}

	pool, err := db.DB()
	if err != nil {
		t.Fatalf("open the pool: %v", err)
	}
	reader, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin the long read: %v", err)
	}
	defer func() { _ = reader.Rollback() }()
	// Holds ACCESS SHARE on user_organizations until the transaction ends.
	if _, err := reader.ExecContext(ctx, `SELECT count(*) FROM user_organizations`); err != nil {
		t.Fatalf("read the memberships: %v", err)
	}

	finished := make(chan error, 1)
	go func() {
		_, err := migrations.Run(context.Background(), db, schema)
		finished <- err
	}()
	const patience = 20 * time.Second
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("the migration altered user_organizations while a reader held it")
		}
		if !strings.Contains(err.Error(), "user_organizations") {
			t.Errorf("the migration failed without saying what it waited for: %v", err)
		}
	case <-time.After(patience):
		_ = reader.Rollback()
		<-finished
		t.Fatalf("the migration was still waiting for user_organizations after %s, and every sign-in queues behind it", patience)
	}

	// Once the reader is done, starting again finishes the job.
	if err := reader.Rollback(); err != nil {
		t.Fatalf("end the long read: %v", err)
	}
	if _, err := migrations.Run(ctx, db, schema); err != nil {
		t.Fatalf("run the migrations again once the read ended: %v", err)
	}
	assertMembershipRolesColumn(t, db)
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
