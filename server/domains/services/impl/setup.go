package impl

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/internal/pkg/config"
	"github.com/gsoultan/gobpm/internal/pkg/crypto"
	"github.com/gsoultan/gobpm/internal/pkg/redaction"
	"github.com/gsoultan/gobpm/server/domains/services/contracts"
	"github.com/gsoultan/gobpm/server/repositories/migrations"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

const minEncryptionKeyLength = 16

// OnSetupCompleteFunc is called after setup succeeds, passing the open target database
// so the application can hot-swap its connection without requiring a restart.
type OnSetupCompleteFunc func(targetDB *gorm.DB)

type setupService struct {
	onSetupComplete OnSetupCompleteFunc
}

func NewSetupService(onSetupComplete OnSetupCompleteFunc) contracts.SetupService {
	return &setupService{onSetupComplete: onSetupComplete}
}

func (s *setupService) GetSetupStatus(_ context.Context) (contracts.SetupStatus, error) {
	return contracts.SetupStatus{
		IsInitialized: config.Exists(config.DefaultConfigPath),
	}, nil
}

func (s *setupService) Setup(ctx context.Context, req contracts.SetupRequest) error {
	status, err := s.GetSetupStatus(ctx)
	if err != nil {
		return err
	}
	if status.IsInitialized {
		return errors.New("system already configured: config.yaml exists")
	}

	if err := validateSetupRequest(req); err != nil {
		return err
	}

	// 1. Open a connection to the TARGET database and seed initial data
	targetDB, cleanup, err := openTargetDatabase(req)
	if err != nil {
		return fmt.Errorf("failed to connect to target database: %w", err)
	}

	// 2. Run migrations on the target database
	if err := migrateTargetDatabase(ctx, targetDB); err != nil {
		cleanup()
		return fmt.Errorf("failed to migrate target database: %w", err)
	}

	// 3. Create Organization, Project, and Admin User in the target database
	if err := seedTargetDatabase(targetDB, req); err != nil {
		cleanup()
		return err
	}

	// 4. Generate and save config.yaml with encrypted connection string
	// We do this AFTER database operations succeed to ensure consistency.
	if err := saveConfiguration(req); err != nil {
		cleanup()
		return err
	}

	// 5. Hot-swap the database connection so the app uses the target DB immediately
	if s.onSetupComplete != nil {
		s.onSetupComplete(targetDB)
	} else {
		cleanup()
	}

	return nil
}

const testConnectionTimeout = 10 * time.Second

func (s *setupService) TestConnection(ctx context.Context, req contracts.TestConnectionRequest) contracts.TestConnectionResult {
	if req.DatabaseDriver == "" {
		return contracts.TestConnectionResult{Success: false, Message: "Database driver is required"}
	}

	fields := config.DatabaseFields{
		Host:       req.DBHost,
		Port:       req.DBPort,
		Username:   req.DBUsername,
		Password:   req.DBPassword,
		DBName:     req.DBName,
		SSLEnabled: req.DBSSLEnabled,
	}
	dsn := config.BuildConnectionString(req.DatabaseDriver, fields)
	dialector := buildDialector(req.DatabaseDriver, dsn)

	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return contracts.TestConnectionResult{Success: false, Message: fmt.Sprintf("Failed to open connection: %s", redaction.RedactError(err))}
	}

	sqlDB, err := db.DB()
	if err != nil {
		return contracts.TestConnectionResult{Success: false, Message: fmt.Sprintf("Failed to get database handle: %s", redaction.RedactError(err))}
	}
	defer sqlDB.Close()

	pingCtx, cancel := context.WithTimeout(ctx, testConnectionTimeout)
	defer cancel()

	if err := sqlDB.PingContext(pingCtx); err != nil {
		return contracts.TestConnectionResult{Success: false, Message: fmt.Sprintf("Connection failed: %s", redaction.RedactError(err))}
	}

	return contracts.TestConnectionResult{Success: true, Message: "Connection successful"}
}

func buildDialector(driver, dsn string) gorm.Dialector {
	switch driver {
	case config.DriverPostgres:
		return postgres.Open(dsn)
	case config.DriverMySQL:
		return mysql.Open(dsn)
	case config.DriverSQLServer:
		return sqlserver.Open(dsn)
	default:
		// Same busy-timeout treatment as the app's own open path; a
		// freshly set-up database must not fail its second request.
		return sqlite.Open(sqliteSetupDSN(dsn))
	}
}

// sqliteSetupDSN mirrors the app's busy-timeout default for databases the
// setup wizard creates.
func sqliteSetupDSN(dsn string) string {
	if strings.Contains(dsn, "_pragma=busy_timeout") {
		return dsn
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	return dsn + separator + "_pragma=busy_timeout(5000)"
}

func validateSetupRequest(req contracts.SetupRequest) error {
	if req.AdminUsername == "" || req.AdminPassword == "" || req.AdminFullName == "" || req.AdminPublicName == "" || req.OrganizationName == "" {
		return errors.New("admin username, password, full name, public name and organization name are required")
	}
	if req.DatabaseDriver == "" {
		return errors.New("database driver is required")
	}
	if req.EncryptionKey == "" {
		return errors.New("encryption key is required")
	}
	if len(req.EncryptionKey) < minEncryptionKeyLength {
		return fmt.Errorf("encryption key must be at least %d characters", minEncryptionKeyLength)
	}
	if req.JWTSecret == "" {
		return errors.New("jwt secret is required")
	}
	return nil
}

func saveConfiguration(req contracts.SetupRequest) error {
	fields := config.DatabaseFields{
		Host:       req.DBHost,
		Port:       req.DBPort,
		Username:   req.DBUsername,
		Password:   req.DBPassword,
		DBName:     req.DBName,
		SSLEnabled: req.DBSSLEnabled,
	}
	connectionString := config.BuildConnectionString(req.DatabaseDriver, fields)

	cfg, err := config.NewConfig(req.DatabaseDriver, connectionString, req.EncryptionKey, req.JWTSecret)
	if err != nil {
		return fmt.Errorf("failed to create configuration: %w", err)
	}

	if err := cfg.Save(config.DefaultConfigPath); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	// Install the key in the running process. Without this the server would
	// have persisted a config it cannot use until the next restart, and every
	// write to an encrypted column would fail with ErrKeyNotConfigured.
	if err := crypto.Configure(req.EncryptionKey); err != nil {
		return fmt.Errorf("failed to install encryption key: %w", err)
	}

	return nil
}

func buildDatabaseFields(req contracts.SetupRequest) config.DatabaseFields {
	return config.DatabaseFields{
		Host:       req.DBHost,
		Port:       req.DBPort,
		Username:   req.DBUsername,
		Password:   req.DBPassword,
		DBName:     req.DBName,
		SSLEnabled: req.DBSSLEnabled,
	}
}

func openTargetDatabase(req contracts.SetupRequest) (*gorm.DB, func(), error) {
	dsn := config.BuildConnectionString(req.DatabaseDriver, buildDatabaseFields(req))
	dialector := buildDialector(req.DatabaseDriver, dsn)

	db, err := gorm.Open(dialector, new(gorm.Config))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open target database: %w", err)
	}

	// SQLite gets one connection, mirroring the app's own open path: pooled
	// connections deadlock on lock upgrades, which busy_timeout cannot help
	// with, and this database is about to be hot-swapped in as the live one.
	if req.DatabaseDriver == config.DriverSQLite || req.DatabaseDriver == "" {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.SetMaxOpenConns(1)
		}
	}

	cleanup := func() {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
	}

	return db, cleanup, nil
}

// migrateTargetDatabase brings the freshly configured database up to the
// current schema.
//
// It runs the same versioned migrations the application runs at boot, rather
// than a bare AutoMigrate. Otherwise setup would create the schema without
// recording a single version, and the first boot afterwards would treat a brand
// new database as one that had never been migrated — replaying every data
// migration over it, including the one that walks every process instance.
func migrateTargetDatabase(ctx context.Context, db *gorm.DB) error {
	// Only the schema migrations. The data migrations need the repository layer,
	// which setup does not have here, and they repair rows written by older
	// versions of the engine — of which a database created seconds ago has none.
	// The first boot runs and records them against empty tables, which costs two
	// queries that find nothing.
	_, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels()))
	return err
}

const defaultProjectName = "Default Project"

func seedTargetDatabase(db *gorm.DB, req contracts.SetupRequest) error {
	return db.Transaction(func(tx *gorm.DB) error {
		orgID := uuid.Must(uuid.NewV7())
		now := time.Now()

		org := models.OrganizationModel{
			Base: models.Base{
				ID:        models.UUID(orgID),
				CreatedAt: now,
			},
			Name: req.OrganizationName,
		}
		if err := tx.Create(&org).Error; err != nil {
			return fmt.Errorf("failed to create organization: %w", err)
		}

		projectName := req.ProjectName
		if projectName == "" {
			projectName = defaultProjectName
		}
		project := models.ProjectModel{
			Base: models.Base{
				ID:        models.UUID(uuid.Must(uuid.NewV7())),
				CreatedAt: now,
			},
			OrganizationID: models.UUID(orgID),
			Name:           projectName,
		}
		if err := tx.Create(&project).Error; err != nil {
			return fmt.Errorf("failed to create project: %w", err)
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.AdminPassword), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("failed to hash password: %w", err)
		}

		admin := models.UserModel{
			Base: models.Base{
				ID:        models.UUID(uuid.Must(uuid.NewV7())),
				CreatedAt: now,
			},
			Username:      req.AdminUsername,
			PasswordHash:  string(hash),
			FullName:      req.AdminFullName,
			DisplayName:   req.AdminPublicName,
			Email:         req.AdminEmail,
			Roles:         []string{"ADMIN"},
			Organizations: []models.OrganizationModel{org},
			Projects:      []models.ProjectModel{project},
		}
		if err := tx.Create(&admin).Error; err != nil {
			return fmt.Errorf("failed to create admin user: %w", err)
		}

		return nil
	})
}
