package migrations_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	stormdb "github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm"
	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// An administrator could put another organization's account into one of their
// groups until AddMembership refused it, and the member list then showed that
// person's name and email to everybody in the group's organization. The refusal
// stops new ones; migration 24 takes out the ones written before it — and only
// those — and logs each one, so an operator has a record of what went.
func TestMigration24RemovesOnlyTheMembershipsThatCrossOrganizations(t *testing.T) {
	db := testutils.SetupTestDB(t)
	conn := testutils.StormConn(db)
	repo := repositories.NewRepository(conn)
	ctx := entities.WithSystemContext(t.Context())
	restoreStormDefaults := func(t *testing.T) {
		t.Helper()
		want, err := storm.Build(model.All()...)
		if err != nil {
			t.Fatalf("build the model: %v", err)
		}
		if _, err := stormdb.EnsureColumnDefaults(ctx, conn.Main(), want); err != nil {
			t.Fatalf("restore the storm column defaults: %v", err)
		}
	}
	migrate := func() {
		t.Helper()
		if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
			t.Fatalf("run the migrations: %v", err)
		}
	}
	migrate()
	// What the application does next at boot. The baseline's AutoMigrate has
	// just taken the defaults off the timestamps storm leaves to the database,
	// and nothing storm writes could be inserted without them.
	restoreStormDefaults(t)

	orgA, orgB := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	for id, name := range map[uuid.UUID]string{orgA: "Organization A", orgB: "Organization B"} {
		if err := repo.Organization().Create(ctx, models.OrganizationModel{Base: models.Base{ID: models.UUID(id)}, Name: name}); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	accounts := serviceimpl.NewUserService(repo, "migration-24-test-secret")
	member, outsider := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	for _, account := range []struct {
		id           uuid.UUID
		username     string
		organization uuid.UUID
	}{{member, "member-of-a", orgA}, {outsider, "member-of-b", orgB}} {
		if err := accounts.CreateUser(ctx, entities.User{
			ID: account.id, Username: account.username, Organizations: []*entities.Organization{{ID: account.organization}},
		}, "a-password-long-enough"); err != nil {
			t.Fatalf("seed %s: %v", account.username, err)
		}
	}
	group := uuid.Must(uuid.NewV7())
	if err := serviceimpl.NewGroupService(repo).CreateGroup(ctx, entities.Group{
		ID: group, Organization: &entities.Organization{ID: orgA}, Name: "Approvers A",
	}); err != nil {
		t.Fatalf("seed a group of organization A: %v", err)
	}
	// Written through the repository, which is how both got in: it checks the
	// group, never the account.
	for _, account := range []uuid.UUID{member, outsider} {
		if err := repo.Group().AddMembership(ctx, account, group); err != nil {
			t.Fatalf("seed a membership: %v", err)
		}
	}

	rewind := func() {
		t.Helper()
		if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = 24`).Error; err != nil {
			t.Fatalf("rewind migration 24: %v", err)
		}
	}
	rewind()
	logs := captureLogs(t)
	migrate()

	if !isMember(t, db, member, group) {
		t.Fatal("the migration removed a membership inside the group's own organization")
	}
	if isMember(t, db, outsider, group) {
		t.Fatal("organization B's account is still in organization A's group")
	}
	record := logs.String()
	if !strings.Contains(record, `"removed":1`) ||
		!strings.Contains(record, `"group_id":"`+group.String()+`"`) ||
		!strings.Contains(record, `"account_id":"`+outsider.String()+`"`) {
		t.Errorf("the log does not record how many were removed and which, by group and account:\n%s", record)
	}
	if strings.Contains(record, member.String()) {
		t.Errorf("the log names a membership that was kept:\n%s", record)
	}

	// Run again from scratch: there is nothing left to remove, and the
	// membership that belongs stays.
	rewind()
	logs.Reset()
	migrate()
	if !isMember(t, db, member, group) {
		t.Fatal("a second run removed a membership inside the group's own organization")
	}
	if record := logs.String(); !strings.Contains(record, `"removed":0`) || strings.Contains(record, `"account_id"`) {
		t.Errorf("a second run removed something, or did not say it removed nothing:\n%s", record)
	}
}

func isMember(t *testing.T, db *gorm.DB, account, group uuid.UUID) bool {
	t.Helper()
	var n int64
	if err := db.WithContext(t.Context()).Raw(
		`SELECT count(*) FROM memberships WHERE user_id = ? AND group_id = ?`, account, group).Scan(&n).Error; err != nil {
		t.Fatalf("read the membership: %v", err)
	}
	return n == 1
}

// captureLogs sends the global logger to a buffer for the rest of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	original := zlog.Logger
	zlog.Logger = zerolog.New(&buf)
	t.Cleanup(func() { zlog.Logger = original })
	return &buf
}
