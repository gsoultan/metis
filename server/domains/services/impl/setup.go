package impl

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/gsoultan/metis/internal/pkg/secrets"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/server/repositories/gorms"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/internal/pkg/crypto"
	"github.com/gsoultan/metis/internal/pkg/dbpool"
	"github.com/gsoultan/metis/internal/pkg/redaction"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	stormdb "github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/storm"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// OnSetupCompleteFunc is called after setup succeeds, passing the open target database
// so the application can hot-swap its connection without requiring a restart.
type OnSetupCompleteFunc func(targetDB *gorm.DB)

// ErrAlreadySetUp refuses setup on an installation that has been set up.
//
// Forbidden rather than a server error: the request was answered correctly, and
// a 5xx would spend the error budget on somebody knocking on a closed door.
var ErrAlreadySetUp = fmt.Errorf("%w: this installation is already set up; sign in instead", apierr.ErrForbidden)

// ErrDatabaseInUse refuses to seed a database that already holds accounts.
var ErrDatabaseInUse = fmt.Errorf(
	"%w: this database already holds a Metis installation, and setup only initializes an empty one; "+
		"to run on it, point DATABASE_URL at it and sign in", apierr.ErrForbidden)

type setupService struct {
	onSetupComplete OnSetupCompleteFunc
	installation    contracts.InstallationProbe
	// setUp is sticky: an installation never becomes un-set-up, and the status
	// is asked on every page load, so once it has been seen the question
	// stops costing a query.
	setUp atomic.Bool
}

// NewSetupService builds the first-run wizard.
//
// installation is the database the server is running on. "Set up" used to mean
// only that config.yaml existed, and a server configured by its environment
// never writes one — so on every container deployment, which runs on a
// read-only root, the wizard stayed open for good: anonymous, in front of a
// database full of somebody's processes. Nil answers from the file alone, for
// tests with no database to ask.
func NewSetupService(onSetupComplete OnSetupCompleteFunc, installation contracts.InstallationProbe) contracts.SetupService {
	return &setupService{onSetupComplete: onSetupComplete, installation: installation}
}

func (s *setupService) GetSetupStatus(ctx context.Context) (contracts.SetupStatus, error) {
	setUp, err := s.isSetUp(ctx)
	if err != nil {
		return contracts.SetupStatus{}, err
	}
	return contracts.SetupStatus{
		IsInitialized:           setUp,
		ConfiguredByEnvironment: configuredByEnvironment(),
	}, nil
}

// isSetUp reports whether this installation has been set up: it has a
// config.yaml, or somebody can already sign in to the database it runs on.
//
// A failure to ask is an error, not a "no". Answering "not set up" because the
// database did not reply would open the wizard at the moment nobody can tell
// whether it should be.
func (s *setupService) isSetUp(ctx context.Context) (bool, error) {
	if s.setUp.Load() {
		return true, nil
	}
	if config.Exists(config.DefaultConfigPath) {
		s.setUp.Store(true)
		return true, nil
	}
	if s.installation == nil {
		return false, nil
	}
	held, err := s.installation.HasAccounts(ctx)
	if err != nil {
		return false, fmt.Errorf("could not tell whether this installation is set up: %w", err)
	}
	if held {
		s.setUp.Store(true)
	}
	return held, nil
}

func (s *setupService) Setup(ctx context.Context, req contracts.SetupRequest) error {
	status, err := s.GetSetupStatus(ctx)
	if err != nil {
		return err
	}
	if status.IsInitialized {
		return ErrAlreadySetUp
	}

	target := setupTargetFor(req)
	if err := validateSetupRequest(req, target); err != nil {
		return err
	}

	// 1. Open a connection to the TARGET database and seed initial data
	targetDB, cleanup, err := openTargetDatabase(target)
	if err != nil {
		return fmt.Errorf("failed to connect to target database: %w", err)
	}

	// 2. Run migrations on the target database
	if err := migrateTargetDatabase(ctx, targetDB, target); err != nil {
		cleanup()
		return fmt.Errorf("failed to migrate target database: %w", err)
	}

	// 3. Create Organization, Project, and Admin User in the target database
	if err := seedTargetDatabase(targetDB, req); err != nil {
		cleanup()
		return err
	}

	// The server already runs on this database with these secrets: there is
	// no file to write, no key to install and nothing to swap to.
	if target.fromEnvironment {
		cleanup()
		s.setUp.Store(true)
		return nil
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

// TestConnection opens a database and reports whether it answered.
//
// Only while the installation is unconfigured. This endpoint is public, because
// the wizard that uses it runs before anyone can sign in — and it takes a host
// and a port from the caller and reports precisely what happened to the attempt.
// On a configured installation that is an unauthenticated port scanner: the
// reply distinguishes "connection refused" from a timeout from an authentication
// failure, which is enough to map whatever network the server sits in.
//
// After setup, the same job is done by the environments endpoint, which requires
// an administrator.
func (s *setupService) TestConnection(ctx context.Context, req contracts.TestConnectionRequest) contracts.TestConnectionResult {
	status, err := s.GetSetupStatus(ctx)
	if err != nil {
		return contracts.TestConnectionResult{Success: false, Message: "Could not determine whether this installation is configured"}
	}
	if status.IsInitialized {
		return contracts.TestConnectionResult{
			Success: false,
			Message: "This installation is already configured. Test a database connection from Settings → Environments.",
		}
	}
	// The wizard has no database step to test when the environment names the
	// database, and a probe nobody needs is only a probe.
	if status.ConfiguredByEnvironment {
		return contracts.TestConnectionResult{
			Success: false,
			Message: "This server's database is set by DATABASE_URL, so there is no connection to test here.",
		}
	}

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
	dialector, err := gorms.Dialector(req.DatabaseDriver, dsn)
	if err != nil {
		return contracts.TestConnectionResult{Success: false, Message: err.Error()}
	}

	db, err := gorm.Open(dialector, gorms.Config())
	if err != nil {
		return contracts.TestConnectionResult{Success: false, Message: fmt.Sprintf("Failed to open connection: %s", redaction.RedactError(err))}
	}

	sqlDB, err := db.DB()
	if err != nil {
		return contracts.TestConnectionResult{Success: false, Message: fmt.Sprintf("Failed to get database handle: %s", redaction.RedactError(err))}
	}
	defer func() {
		// A connection test that leaks its own pool is a connection test that
		// exhausts the server it was checking.
		if err := sqlDB.Close(); err != nil {
			log.Warn().Err(err).Msg("Could not close the connection opened to test the database")
		}
	}()

	pingCtx, cancel := context.WithTimeout(ctx, testConnectionTimeout)
	defer cancel()

	if err := sqlDB.PingContext(pingCtx); err != nil {
		return contracts.TestConnectionResult{Success: false, Message: fmt.Sprintf("Connection failed: %s", redaction.RedactError(err))}
	}

	return contracts.TestConnectionResult{Success: true, Message: "Connection successful"}
}

func validateSetupRequest(req contracts.SetupRequest, target setupTarget) error {
	if req.AdminUsername == "" || req.AdminPassword == "" || req.AdminFullName == "" || req.AdminPublicName == "" || req.OrganizationName == "" {
		return apierr.Invalidf("admin username, password, full name, public name and organization name are required")
	}
	// The environment's database and secrets were checked when the server
	// started — it refuses weak ones — and whatever the request says about
	// either is not what setup will use.
	if target.fromEnvironment {
		return nil
	}
	return validateWizardConfiguration(req)
}

// validateWizardConfiguration checks the database and the secrets the wizard
// is about to write into config.yaml.
func validateWizardConfiguration(req contracts.SetupRequest) error {
	if req.DatabaseDriver == "" {
		return apierr.Invalidf("database driver is required")
	}
	if !config.SupportedDriver(req.DatabaseDriver) {
		return apierr.Invalidf("this runs on PostgreSQL; %q is not a database engine it supports", req.DatabaseDriver)
	}
	// Validated here, before saveConfiguration encrypts anything with the key.
	//
	// The wizard used to apply a weaker rule of its own — sixteen characters
	// for the encryption key, nothing at all for the JWT secret beyond being
	// present — while the boot path refused anything under thirty-two. So a
	// wizard run could succeed, write the config, and leave a server that
	// refused to start on the next restart: configured successfully, and
	// permanently unable to come back. Sharing the rule is what stops the two
	// disagreeing.
	//
	// This is also the right moment for it. A new installation is the one point
	// where the key can still be chosen freely; once data has been encrypted
	// with it, a weak key has no safe remedy.
	if !secrets.Allowed() {
		if err := secrets.Validate("encryption key", req.EncryptionKey); err != nil {
			return fmt.Errorf("%w: %w", apierr.ErrInvalidArgument, err)
		}
		if err := secrets.Validate("JWT secret", req.JWTSecret); err != nil {
			return fmt.Errorf("%w: %w", apierr.ErrInvalidArgument, err)
		}
	}
	if req.EncryptionKey == "" {
		return apierr.Invalidf("encryption key is required")
	}
	if req.JWTSecret == "" {
		return apierr.Invalidf("jwt secret is required")
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

func openTargetDatabase(target setupTarget) (*gorm.DB, func(), error) {
	dialector, err := gorms.Dialector(target.driver, target.dsn)
	if err != nil {
		return nil, nil, err
	}

	db, err := gorm.Open(dialector, gorms.Config())
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open target database: %w", err)
	}

	// Sized the same way the app's own open path sizes it — this database is
	// about to be hot-swapped in as the live one, so it must not run the rest
	// of its life on a pool nobody configured.
	dbpool.Apply(db)

	cleanup := func() {
		sqlDB, err := db.DB()
		if err != nil {
			log.Warn().Err(err).Msg("Could not reach the database handle to close it")
			return
		}
		if err := sqlDB.Close(); err != nil {
			log.Warn().Err(err).Msg("Could not close the database connection pool")
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
func migrateTargetDatabase(ctx context.Context, db *gorm.DB, target setupTarget) error {
	// Only the schema migrations. The data migrations need the repository layer,
	// which setup does not have here, and they repair rows written by older
	// versions of the engine — of which a database created seconds ago has none.
	// The first boot runs and records them against empty tables, which costs two
	// queries that find nothing.
	if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
		return err
	}
	return ensureStormSchema(ctx, target.dsn)
}

// ensureStormSchema creates the tables the GORM migrations do not describe, in
// the database the wizard has just configured.
//
// The same two steps the application performs at boot, run here because setup
// writes into this database immediately afterwards — the admin's organization
// membership lands in a join table the GORM models no longer declare, and
// without this the very first insert of a fresh installation fails with
// "relation user_organizations does not exist".
//
// A second connection rather than the GORM one, because these are storm's own
// DDL and its schema builder speaks pgx.
func ensureStormSchema(ctx context.Context, dsn string) error {
	want, err := storm.Build(model.All()...)
	if err != nil {
		return fmt.Errorf("the model layer does not build: %w", err)
	}
	pool, err := pgxpool.New(ctx, config.PostgresURL(dsn))
	if err != nil {
		return fmt.Errorf("could not open the target database for the storm schema: %w", err)
	}
	defer pool.Close()

	if _, err := stormdb.EnsureTables(ctx, pool, want); err != nil {
		return fmt.Errorf("could not create the storm tables: %w", err)
	}
	if _, err := stormdb.EnsureColumnDefaults(ctx, pool, want); err != nil {
		return fmt.Errorf("could not reconcile the storm column defaults: %w", err)
	}
	return nil
}

const defaultProjectName = "Default Project"

// setupAdvisoryLock serialises refuseExistingInstallation's check and the seed
// that follows it, across concurrent requests and replicas. Any fixed key
// would do; this one is "metis" in ASCII.
const setupAdvisoryLock int64 = 0x6d65746973

// refuseExistingInstallation stops setup seeding a database that is in use.
//
// Setup is anonymous by necessity — nobody can sign in before it — so it must
// only ever create the first administrator of an empty database. Without this,
// anyone who could reach the wizard and knew a working connection string could
// mint an administrator inside an installation that was already running. The
// lock makes the check and the seed one step: two first runs racing each other
// would otherwise both find the database empty.
func refuseExistingInstallation(tx *gorm.DB) error {
	if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", setupAdvisoryLock).Error; err != nil {
		return fmt.Errorf("could not take the setup lock: %w", err)
	}
	var inUse bool
	if err := tx.Raw("SELECT EXISTS (SELECT 1 FROM users)").Scan(&inUse).Error; err != nil {
		return fmt.Errorf("could not tell whether this database is already in use: %w", err)
	}
	if inUse {
		return ErrDatabaseInUse
	}
	return nil
}

func seedTargetDatabase(db *gorm.DB, req contracts.SetupRequest) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := refuseExistingInstallation(tx); err != nil {
			return err
		}
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
			Username:     req.AdminUsername,
			PasswordHash: string(hash),
			FullName:     req.AdminFullName,
			DisplayName:  req.AdminPublicName,
			Email:        req.AdminEmail,
			Roles:        []string{"ADMIN"},
		}
		if err := tx.Create(&admin).Error; err != nil {
			return fmt.Errorf("failed to create admin user: %w", err)
		}

		// The memberships, written explicitly.
		//
		// They used to be GORM associations on the struct above, which wrote
		// the join rows as a side effect of creating the user. The join tables
		// belong to the storm model now and the association tags are gone, so
		// the side effect went with them — silently: setup succeeded, the admin
		// existed, and signing in showed "authenticated principal has no
		// organization membership" on every page.
		//
		// Raw SQL because this runs on the database the wizard is configuring,
		// which is not the one the repositories are connected to yet.
		if err := tx.Exec(
			`INSERT INTO user_organizations (user_id, organization_id) VALUES (?, ?)`,
			admin.ID, org.ID).Error; err != nil {
			return fmt.Errorf("failed to put the admin in the organization: %w", err)
		}
		if err := tx.Exec(
			`INSERT INTO user_projects (user_id, project_id) VALUES (?, ?)`,
			admin.ID, project.ID).Error; err != nil {
			return fmt.Errorf("failed to put the admin in the project: %w", err)
		}

		return nil
	})
}
