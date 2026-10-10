package setup_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	stormdb "github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/gorms"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// TestSetupLeavesTheAdminAbleToSeeSomething is the check the wizard did not
// have, and the reason it needed one.
//
// Setup creating an account nobody can use is not a failure it can report: it
// returns nil, the account exists, the password works, and every page answers
// "authenticated principal has no organization membership". The only way to
// know is to sign in afterwards, which is what this does.
//
// It caught a real one. The memberships were written as a side effect of a GORM
// association on the user struct; when the join tables moved to the storm model
// the association tags went with them, and so did the side effect — silently,
// on every fresh installation.
//
// Runs against its own database rather than the shared test schema, because
// that is what setup does: it opens the database named in the request and
// migrates it from nothing.
func TestSetupLeavesTheAdminAbleToSeeSomething(t *testing.T) {
	host, port, user, password := postgresCredentials(t)
	database := "metis_setup_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")

	admin := createDatabase(t, host, port, user, password, database)
	defer dropDatabase(t, admin, database)

	// config.yaml is written relative to the working directory, so the test is
	// run from a temporary one: setup's whole job is to write that file, and
	// writing it into the repository would leave the next run "already
	// configured".
	t.Chdir(t.TempDir())

	svc := impl.NewSetupService(nil)
	ctx := context.Background()
	logged := serverLog(t)
	if err := svc.Setup(ctx, contracts.SetupRequest{
		AdminUsername:    "admin",
		AdminPassword:    "correct-horse-battery-staple",
		AdminFullName:    "Development Admin",
		AdminPublicName:  "Admin",
		AdminEmail:       "admin@example.invalid",
		OrganizationName: "Example Co",
		ProjectName:      "Sample Project",
		DatabaseDriver:   "postgres",
		DBHost:           host,
		DBPort:           port,
		DBUsername:       user,
		DBPassword:       password,
		DBName:           database,
		EncryptionKey:    "an-encryption-key-of-sufficient-length",
		JWTSecret:        "a-jwt-secret-of-sufficient-length-to-pass-validation",
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	conn, err := stormdb.Open(ctx, fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable", user, password, host, port, database))
	if err != nil {
		t.Fatalf("open the database setup just configured: %v", err)
	}
	t.Cleanup(conn.Close)

	repo := repositories.NewRepository(conn)
	account, err := repo.User().GetByUsername(entities.WithSystemContext(ctx), "admin")
	if err != nil {
		t.Fatalf("setup reported success and the account it created cannot be read: %v", err)
	}

	// The membership is what every scoped read resolves a tenant from. Without
	// it the account signs in and sees nothing, on every page, with no error
	// that names setup as the cause.
	if len(account.Organizations) == 0 {
		t.Fatal("the admin setup created belongs to no organization; signing in shows nothing, everywhere")
	}
	if len(account.Projects) == 0 {
		t.Error("the admin setup created belongs to no project")
	}

	// The first administrator is an account that holds the administrator
	// role, written by set-up itself and not through the account service. It
	// is attributable as every other is: one line of the same shape, naming
	// no actor — nobody is signed in to an installation that has no account —
	// and saying that set-up made it.
	var created []map[string]any
	for _, line := range strings.Split(logged.String(), "\n") {
		entry := map[string]any{}
		if json.Unmarshal([]byte(line), &entry) == nil && entry["message"] == "An account was created." {
			created = append(created, entry)
		}
	}
	if len(created) != 1 {
		t.Fatalf("set-up logged %d lines for the account it created, want one: %s", len(created), logged.String())
	}
	line := created[0]
	if line["actor"] != "" || line["actor_id"] != "" || line["made_through"] != "set-up" || line["target"] != "admin" ||
		line["target_id"] != uuid.UUID(account.ID).String() || line["organization"] != uuid.UUID(account.Organizations[0].ID).String() ||
		!reflect.DeepEqual(line["roles_after"], []any{entities.RoleAdmin}) || !reflect.DeepEqual(line["roles_before"], []any{}) ||
		!reflect.DeepEqual(line["member_of"], []any{uuid.UUID(account.Organizations[0].ID).String()}) {
		t.Fatalf("the line for the first administrator is %v; want it to name the account, its organization and the administrator role, "+
			"no actor, and set-up as what made it", line)
	}
	if strings.Contains(logged.String(), "correct-horse-battery-staple") {
		t.Fatal("the log holds the first administrator's password")
	}
}

// serverLog keeps what the server logs from here to the end of the test. The
// tests of this package do not run in parallel.
func serverLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	kept := &bytes.Buffer{}
	was := log.Logger
	log.Logger = zerolog.New(kept)
	t.Cleanup(func() { log.Logger = was })
	return kept
}

// postgresCredentials pulls the pieces out of the configured DSN.
//
// Setup takes host, port, user, password and database separately — it builds
// its own connection string — so the test has to take the DSN apart to hand
// them over.
func postgresCredentials(t *testing.T) (host string, port int, user, password string) {
	t.Helper()
	dsn := envvar.Get(testutils.PostgresDSNEnv)
	if dsn == "" {
		t.Skipf("set %s to run this against a live PostgreSQL instance", testutils.PostgresDSNEnv)
	}
	fields := map[string]string{}
	for _, pair := range strings.Fields(dsn) {
		key, value, ok := strings.Cut(pair, "=")
		if ok {
			fields[key] = value
		}
	}
	if _, err := fmt.Sscanf(fields["port"], "%d", &port); err != nil || port == 0 {
		port = 5432
	}
	return fields["host"], port, fields["user"], fields["password"]
}

func createDatabase(t *testing.T, host string, port int, user, password, database string) *gorm.DB {
	t.Helper()
	admin, err := gorms.Open("postgres", fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=postgres sslmode=disable", host, port, user, password))
	if err != nil {
		t.Fatalf("open the maintenance connection: %v", err)
	}
	if err := admin.Exec("CREATE DATABASE " + database).Error; err != nil {
		t.Fatalf("create %s: %v", database, err)
	}
	return admin
}

func dropDatabase(t *testing.T, admin *gorm.DB, database string) {
	t.Helper()
	// The connections setup opened are closed by then, but a pool that has not
	// finished releasing them makes DROP DATABASE fail — and a leaked database
	// on a shared server is somebody else's problem tomorrow.
	_ = admin.Exec("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = ? AND pid <> pg_backend_pid()", database).Error
	if err := admin.Exec("DROP DATABASE IF EXISTS " + database).Error; err != nil {
		t.Logf("could not drop %s: %v", database, err)
	}
	if sqlDB, err := admin.DB(); err == nil {
		_ = sqlDB.Close()
	}
}
