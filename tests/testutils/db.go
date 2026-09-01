package testutils

import (
	"testing"

	"github.com/gsoultan/metis/server/repositories/gorms"
	"github.com/gsoultan/metis/server/repositories/migrations"

	"github.com/glebarez/sqlite"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"gorm.io/gorm"
)

// testEncryptionPassphrase is the at-rest key used by every test database.
//
// crypto has no default key by design — process/task variables cannot be
// written until one is configured — so tests must install one explicitly, just
// as the application does at startup.
const testEncryptionPassphrase = "test-only-encryption-passphrase"

func SetupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	if err := crypto.Configure(testEncryptionPassphrase); err != nil {
		t.Fatalf("failed to configure test encryption key: %v", err)
	}

	db, err := gorm.Open(sqlite.Open(":memory:"), gorms.Config())
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	// The model list is shared with the PostgreSQL and MySQL helpers: keeping a
	// second copy here guaranteed the three dialects would drift apart on the
	// next model added.
	if err := db.AutoMigrate(migrationModels()...); err != nil {
		t.Fatalf("failed to migrate db: %v", err)
	}
	ensureVersionIndexes(t, db)
	return db
}

// ensureVersionIndexes adds the constraints the migration runner installs but
// AutoMigrate does not.
//
// The unique index on a definition's (project_id, key, version) is declared by a
// migration rather than a struct tag — see ProcessDefinitionModel for why — so a
// schema built straight from the models is missing it. A test database without
// it would accept two definitions claiming the same version and quietly prove
// the opposite of what the version allocator tests assert.
func ensureVersionIndexes(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := migrations.EnsureVersionIndexes(db, migrationModels()); err != nil {
		t.Fatalf("failed to create version indexes: %v", err)
	}
}
