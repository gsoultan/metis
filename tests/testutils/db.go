package testutils

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gsoultan/gobpm/internal/pkg/crypto"
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

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
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
	return db
}
