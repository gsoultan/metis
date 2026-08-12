package testutils

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gsoultan/gobpm/internal/pkg/crypto"
	models2 "github.com/gsoultan/gobpm/server/repositories/models"
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
	err = db.AutoMigrate(
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
	)
	if err != nil {
		t.Fatalf("failed to migrate db: %v", err)
	}
	return db
}
