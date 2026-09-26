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

// identityIndex is the unique index over an account's link to its identity
// provider, named as the storm model names it.
const identityIndex = "uq_users_identity_issuer_identity_subject"

// Migration 27 is only reachable from the old schema: a fresh install gets the
// identity columns and their index from the model. This puts users back the way
// an upgrading installation has it — no identity columns — with an account
// already in it, and runs the migration at it.
func TestMigration27LinksAnAccountToItsIdentityOnAnExistingTable(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	migrate := func() {
		t.Helper()
		if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
			t.Fatalf("run the migrations: %v", err)
		}
	}
	migrate()
	// A fresh install has the index from the model, and it must already be the
	// partial one: a deleted account must not hold its identity for ever.
	assertIdentityIndexCoversLiveRows(t, db)

	for _, stmt := range []string{
		`DROP INDEX IF EXISTS ` + identityIndex,
		`ALTER TABLE users DROP COLUMN IF EXISTS identity_issuer`,
		`ALTER TABLE users DROP COLUMN IF EXISTS identity_subject`,
		`DELETE FROM schema_migrations WHERE version = 27`,
	} {
		if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
			t.Fatalf("restore the old schema (%s): %v", stmt, err)
		}
	}
	legacy := insertAccount(t, db, "legacy-local", "", "")

	migrate()
	migrate() // and a second run finds nothing left to do

	var linked int64
	if err := db.WithContext(ctx).Raw(
		`SELECT count(*) FROM users WHERE id = ? AND (identity_issuer IS NOT NULL OR identity_subject IS NOT NULL)`,
		legacy).Scan(&linked).Error; err != nil {
		t.Fatalf("read the account created before the migration: %v", err)
	}
	if linked != 0 {
		t.Fatal("the migration linked an existing local account to an identity nobody vouched for")
	}
	assertIdentityIndexCoversLiveRows(t, db)

	// One live account per identity.
	const issuer, subject = "https://id.example.com", "subject-1"
	first := insertAccount(t, db, "first-linked", issuer, subject)
	if err := tryInsertAccount(db, "second-linked", issuer, subject); err == nil {
		t.Fatal("two live accounts were linked to one identity")
	}

	// A deleted account does not hold the identity: the person's next sign-in
	// is given a new account rather than refused for ever.
	if err := db.WithContext(ctx).Exec(
		`UPDATE users SET deleted_at = ? WHERE id = ?`, time.Now(), first).Error; err != nil {
		t.Fatalf("delete the first linked account: %v", err)
	}
	insertAccount(t, db, "relinked", issuer, subject)
}

// assertIdentityIndexCoversLiveRows checks the index is unique and scoped to
// the rows that are not deleted.
func assertIdentityIndexCoversLiveRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	var definition string
	if err := db.WithContext(t.Context()).Raw(
		`SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() AND indexname = ?`,
		identityIndex).Scan(&definition).Error; err != nil {
		t.Fatalf("read the identity index: %v", err)
	}
	if definition == "" {
		t.Fatalf("%s does not exist", identityIndex)
	}
	if !strings.Contains(definition, "UNIQUE") || !strings.Contains(definition, "(identity_issuer, identity_subject)") {
		t.Fatalf("%s is not unique over (identity_issuer, identity_subject): %s", identityIndex, definition)
	}
	if !strings.Contains(definition, "WHERE (deleted_at IS NULL)") {
		t.Fatalf("%s covers deleted accounts too, so a deleted account would hold its identity for ever: %s",
			identityIndex, definition)
	}
}

// insertAccount writes an account the way a release that predates the link
// would have, when issuer is empty, and linked to (issuer, subject) otherwise.
func insertAccount(t *testing.T, db *gorm.DB, username, issuer, subject string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := insertAccountWithID(db, id, username, issuer, subject); err != nil {
		t.Fatalf("insert account %s: %v", username, err)
	}
	return id
}

func tryInsertAccount(db *gorm.DB, username, issuer, subject string) error {
	return insertAccountWithID(db, uuid.New(), username, issuer, subject)
}

func insertAccountWithID(db *gorm.DB, id uuid.UUID, username, issuer, subject string) error {
	now := time.Now()
	columns := `id, created_at, updated_at, username, password_hash, full_name, display_name, organization, email, roles`
	values := `?, ?, ?, ?, '', '', '', '', '', '[]'`
	args := []any{id, now, now, username}
	if issuer != "" {
		columns += ", identity_issuer, identity_subject"
		values += ", ?, ?"
		args = append(args, issuer, subject)
	}
	return db.Exec("INSERT INTO users ("+columns+") VALUES ("+values+")", args...).Error
}
