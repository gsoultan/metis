package setup_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	stormdb "github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/gorms"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/storm"
)

// heldInstallation answers the one question setup asks of the running database.
type heldInstallation bool

func (h heldInstallation) HasAccounts(context.Context) (bool, error) { return bool(h), nil }

// asWizard clears what a server configured by its environment is started with,
// so a test of the wizard does not change behaviour on a machine that happens
// to export them.
func asWizard(t *testing.T) {
	t.Helper()
	for _, name := range []string{"DATABASE_URL", "ENCRYPTION_KEY", "JWT_SECRET"} {
		t.Setenv(name, "")
	}
}

// The wizard is anonymous by necessity, and it used to count as closed only
// once config.yaml existed. A server started from DATABASE_URL never writes
// one — the container deployments run on a read-only root, where it could not —
// so on every one of them setup stayed open in front of a running installation:
// anyone could re-run it, and with the database credentials the evaluation
// stack publishes, mint an administrator inside it.
func TestTheWizardClosesOnceTheDatabaseItRunsOnHoldsAnInstallation(t *testing.T) {
	asWizard(t)
	t.Chdir(t.TempDir())

	svc := impl.NewSetupService(nil, heldInstallation(true))

	status, err := svc.GetSetupStatus(t.Context())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.IsInitialized {
		t.Fatal("a server whose database already has accounts reported itself unconfigured; the wizard is open to anyone")
	}

	err = svc.Setup(t.Context(), contracts.SetupRequest{
		AdminUsername: "intruder", AdminPassword: "intruder-password", AdminFullName: "I",
		AdminPublicName: "I", OrganizationName: "Mine now", DatabaseDriver: "postgres",
	})
	if !errors.Is(err, impl.ErrAlreadySetUp) {
		t.Fatalf("setup on a running installation must be refused as already set up, got %v", err)
	}

	result := svc.TestConnection(t.Context(), contracts.TestConnectionRequest{
		DatabaseDriver: "postgres", DBHost: "127.0.0.1", DBPort: 1, DBName: "probe",
	})
	if result.Success || !strings.Contains(result.Message, "already configured") {
		t.Fatalf("the connection probe must close with the wizard, got %+v", result)
	}
}

// An empty database is still a first run: the probe must not close the wizard
// before anybody has been able to use it.
func TestTheWizardStaysOpenOnAnEmptyDatabase(t *testing.T) {
	asWizard(t)
	t.Chdir(t.TempDir())

	status, err := impl.NewSetupService(nil, heldInstallation(false)).GetSetupStatus(t.Context())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.IsInitialized {
		t.Fatal("an empty database reported itself set up; nobody could ever create the first administrator")
	}
}

// Setup seeds only an empty database, whatever the status says.
//
// The status check guards the server's own database. This guards the one the
// request names, which can be any database the caller has credentials for — a
// fresh server pointed at a production database would otherwise add a second
// organization and an administrator of the caller's choosing to it.
func TestSetupWillNotSeedADatabaseThatIsAlreadyInUse(t *testing.T) {
	asWizard(t)
	host, port, user, password := postgresCredentials(t)
	database := "metis_setup_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	admin := createDatabase(t, host, port, user, password, database)
	defer dropDatabase(t, admin, database)

	request := func(org, username string) contracts.SetupRequest {
		return contracts.SetupRequest{
			AdminUsername: username, AdminPassword: "correct-horse-battery-staple",
			AdminFullName: "Admin", AdminPublicName: "Admin", AdminEmail: "admin@example.invalid",
			OrganizationName: org, DatabaseDriver: "postgres",
			DBHost: host, DBPort: port, DBUsername: user, DBPassword: password, DBName: database,
			EncryptionKey: strongSecret(t), JWTSecret: strongSecret(t),
		}
	}

	t.Chdir(t.TempDir())
	if err := impl.NewSetupService(nil, nil).Setup(t.Context(), request("First Co", "first-admin")); err != nil {
		t.Fatalf("the first setup of an empty database: %v", err)
	}

	// A second server, never set up, pointed at the same database.
	t.Chdir(t.TempDir())
	err := impl.NewSetupService(nil, nil).Setup(t.Context(), request("Second Co", "second-admin"))
	if !errors.Is(err, impl.ErrDatabaseInUse) {
		t.Fatalf("setup seeded a database that already held an installation, got %v", err)
	}

	repo := repositories.NewRepository(openStorm(t, host, port, user, password, database))
	if _, err := repo.User().GetByUsername(entities.WithSystemContext(t.Context()), "second-admin"); err == nil {
		t.Fatal("the refused setup still created its administrator")
	}
}

// A server started from its environment is configured already, except for the
// people in it. The wizard used to ask for the database and the secrets again,
// seed the administrator, and then fail writing config.yaml on the read-only
// root every container deployment runs on — so the first run never finished,
// and the account it had created was left behind in a database that still said
// it was not set up.
func TestAServerConfiguredByItsEnvironmentSeedsItsOwnDatabaseAndWritesNothing(t *testing.T) {
	host, port, user, password := postgresCredentials(t)
	database := "metis_setup_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	admin := createDatabase(t, host, port, user, password, database)
	defer dropDatabase(t, admin, database)

	// A URL rather than keyword=value: an empty password in the keyword form
	// swallows the field after it, and a DSN with no dbname quietly means the
	// database named after the user.
	t.Setenv("DATABASE_URL", postgresURL(host, port, user, password, database))
	t.Setenv("ENCRYPTION_KEY", strongSecret(t))
	t.Setenv("JWT_SECRET", strongSecret(t))
	readOnlyWorkingDirectory(t)

	conn := openStorm(t, host, port, user, password, database)
	migrateAsBootDoes(t, conn, host, port, user, password, database)
	repo := repositories.NewRepository(conn)
	svc := impl.NewSetupService(nil, repo.User())

	before, err := svc.GetSetupStatus(t.Context())
	if err != nil {
		t.Fatalf("status before: %v", err)
	}
	if before.IsInitialized || !before.ConfiguredByEnvironment {
		t.Fatalf("a fresh server started from its environment should ask only for its people, got %+v", before)
	}

	// Only the people: the environment has said everything else.
	if err := svc.Setup(t.Context(), contracts.SetupRequest{
		AdminUsername: "admin", AdminPassword: "correct-horse-battery-staple",
		AdminFullName: "Admin", AdminPublicName: "Admin", AdminEmail: "admin@example.invalid",
		OrganizationName: "Example Co",
	}); err != nil {
		t.Fatalf("setup from the environment: %v", err)
	}

	if config.Exists(config.DefaultConfigPath) {
		t.Fatal("setup wrote a config.yaml; on the next restart it would outrank DATABASE_URL")
	}
	account, err := repo.User().GetByUsername(entities.WithSystemContext(t.Context()), "admin")
	if err != nil {
		t.Fatalf("the administrator was not created in the server's own database: %v", err)
	}
	if len(account.Organizations) == 0 {
		t.Fatal("the administrator belongs to no organization; signing in would show nothing")
	}

	after, err := svc.GetSetupStatus(t.Context())
	if err != nil {
		t.Fatalf("status after: %v", err)
	}
	if !after.IsInitialized {
		t.Fatal("the wizard is still open after it finished")
	}
}

// readOnlyWorkingDirectory runs the test from a directory nothing can be
// written to, which is what a container's read-only root is to the server.
func readOnlyWorkingDirectory(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	// Read and search, no write. Nothing can be created in it, so it stays empty
	// and TempDir's cleanup removes it without the write bit back: removing an
	// empty directory is a write to its parent.
	// #nosec G302 -- a directory, not a file: without its search bit it cannot
	// be a working directory at all, and it grants nobody but the owner anything.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("make the working directory read-only: %v", err)
	}
}

// migrateAsBootDoes brings a fresh database to the schema a server has before
// it answers its first request: the versioned migrations, then storm's tables.
func migrateAsBootDoes(t *testing.T, conn *stormdb.Conn, host string, port int, user, password, database string) {
	t.Helper()
	gormDB, err := gorms.Open("postgres", postgresURL(host, port, user, password, database))
	if err != nil {
		t.Fatalf("open %s to migrate it: %v", database, err)
	}
	t.Cleanup(func() {
		if sqlDB, err := gormDB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if _, err := migrations.Run(t.Context(), gormDB, migrations.Schema(models.MigrationModels())); err != nil {
		t.Fatalf("migrate %s: %v", database, err)
	}
	want, err := storm.Build(model.All()...)
	if err != nil {
		t.Fatalf("build the storm model: %v", err)
	}
	if _, err := stormdb.EnsureTables(t.Context(), conn.Main(), want); err != nil {
		t.Fatalf("create the storm tables: %v", err)
	}
}

func postgresURL(host string, port int, user, password, database string) string {
	target := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password),
		Host:     net.JoinHostPort(host, strconv.Itoa(port)),
		Path:     "/" + database,
		RawQuery: "sslmode=disable",
	}
	return target.String()
}

func openStorm(t *testing.T, host string, port int, user, password, database string) *stormdb.Conn {
	t.Helper()
	conn, err := stormdb.Open(context.Background(), postgresURL(host, port, user, password, database))
	if err != nil {
		t.Fatalf("open %s: %v", database, err)
	}
	t.Cleanup(conn.Close)
	return conn
}

// strongSecret is generated rather than written down: a literal of the right
// shape is indistinguishable from a real key to a secret scanner.
func strongSecret(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("generate a secret: %v", err)
	}
	return hex.EncodeToString(buf)
}
