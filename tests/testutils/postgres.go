package testutils

import (
	"os"
	"testing"

	"github.com/gsoultan/gobpm/internal/pkg/crypto"
	models2 "github.com/gsoultan/gobpm/server/repositories/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// PostgresDSNEnv names the environment variable holding a DSN for a live
// PostgreSQL instance.
//
// The rest of the suite runs on in-memory SQLite with a single connection, which
// serialises every transaction. That is fine for behaviour but it cannot show
// anything about concurrency, and it does not exercise the SQL a real deployment
// runs. Tests that need either of those ask for this DSN and skip without it, so
// the default gate stays hermetic.
const PostgresDSNEnv = "GOBPM_TEST_POSTGRES_DSN"

// SetupPostgresDB opens the PostgreSQL instance named by GOBPM_TEST_POSTGRES_DSN
// and migrates a schema into it, skipping the test when no DSN is configured.
//
// maxConns controls the connection pool. More than one connection is what makes
// genuine concurrency possible, which is the whole point of reaching for a real
// database here.
func SetupPostgresDB(t *testing.T, maxConns int) *gorm.DB {
	t.Helper()

	dsn := os.Getenv(PostgresDSNEnv)
	if dsn == "" {
		t.Skipf("set %s to run this against a live PostgreSQL instance", PostgresDSNEnv)
	}

	if err := crypto.Configure(testEncryptionPassphrase); err != nil {
		t.Fatalf("failed to configure test encryption key: %v", err)
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open postgres: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(maxConns)

	// Each test gets its own schema so runs cannot see one another's rows and the
	// whole lot can be dropped afterwards.
	schema := "gobpm_test_" + sanitiseSchemaName(t.Name())
	if err := db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error; err != nil {
		t.Fatalf("failed to drop schema: %v", err)
	}
	if err := db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}
	if err := db.Exec("SET search_path TO " + schema).Error; err != nil {
		t.Fatalf("failed to set search_path: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error
		_ = sqlDB.Close()
	})

	// Reopen with the schema pinned in the DSN so every pooled connection lands
	// in it, not just the one that ran SET search_path.
	scoped, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to reopen postgres on the test schema: %v", err)
	}
	scopedDB, err := scoped.DB()
	if err != nil {
		t.Fatalf("failed to get scoped sql.DB: %v", err)
	}
	scopedDB.SetMaxOpenConns(maxConns)
	t.Cleanup(func() { _ = scopedDB.Close() })

	if err := scoped.AutoMigrate(migrationModels()...); err != nil {
		t.Fatalf("failed to migrate postgres schema: %v", err)
	}
	return scoped
}

func migrationModels() []any {
	return []any{
		&models2.OrganizationModel{},
		&models2.ProcessInstanceModel{},
		&models2.TaskModel{},
		&models2.ProcessDefinitionModel{},
		&models2.ProjectModel{},
		&models2.AuditModel{},
		&models2.JobModel{},
		&models2.IncidentModel{},
		&models2.ExternalTaskModel{},
		&models2.Subscription{},
		&models2.DecisionDefinitionModel{},
		&models2.Connector{},
		&models2.ConnectorInstance{},
		&models2.UserModel{},
		&models2.GroupModel{},
		&models2.MembershipModel{},
		&models2.CompensatableActivityModel{},
		&models2.VariableSnapshotModel{},
	}
}

// sanitiseSchemaName reduces a Go test name to something PostgreSQL accepts as
// an identifier.
func sanitiseSchemaName(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		default:
			out = append(out, '_')
		}
	}
	if len(out) > 50 {
		out = out[:50]
	}
	return string(out)
}
