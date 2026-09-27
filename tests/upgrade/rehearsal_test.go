// Package upgrade rehearses the upgrade path on a database that already has
// data in it.
//
// The rest of the suite starts from an empty schema, which proves a fresh
// install works and says nothing about the case every existing installation is
// in. The migrations that rename columns are only reachable *from* the old
// shape: a fresh install creates the new names and skips them, so they were
// written, reviewed, and never executed against a table that had the old ones.
//
// So this builds the old shape deliberately, puts rows in it, runs the real
// boot sequence over it, and then reads those rows back through the repositories
// that were rewritten. Anything the upgrade breaks shows up here as a failure
// rather than on somebody's first restart.
package upgrade_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	stormdb "github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/model"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm"
	"gorm.io/gorm"
)

// TestAnExistingDatabaseUpgrades is the rehearsal.
//
// It fails if any of four things is true: a rename migration cannot run against
// the shape it was written for, the storm-only tables are not created, the
// column defaults storm writes against are not added, or a row written before
// the upgrade is not readable after it.
func TestAnExistingDatabaseUpgrades(t *testing.T) {
	gormDB, conn := testutils.SetupTestStore(t)
	ctx := entities.WithSystemContext(context.Background())

	seeded := ageTheDatabase(t, gormDB)

	// The boot sequence, in the order internal/app runs it.
	result, err := migrations.Run(ctx, gormDB, migrations.Schema(models.MigrationModels()))
	if err != nil {
		t.Fatalf("the migrations refused an existing database: %v", err)
	}
	t.Logf("applied migrations: %v", result.Applied)

	want, err := storm.Build(model.All()...)
	if err != nil {
		t.Fatalf("build the model: %v", err)
	}
	created, err := stormdb.EnsureTables(ctx, conn.Main(), want)
	if err != nil {
		t.Fatalf("create the storm tables: %v", err)
	}
	defaulted, err := stormdb.EnsureColumnDefaults(ctx, conn.Main(), want)
	if err != nil {
		t.Fatalf("reconcile the column defaults: %v", err)
	}
	t.Logf("created %d tables, defaulted %d columns", len(created), len(defaulted))

	// Nothing left disagreeing about a column that a query would name.
	drift, err := stormdb.ReportDrift(ctx, conn.Main(), want)
	if err != nil {
		t.Fatalf("report drift: %v", err)
	}
	for _, statement := range drift {
		if breaking(statement) {
			t.Errorf("after the upgrade the schema still disagrees with the model: %s", statement)
		}
	}

	assertTheOldRowsSurvived(t, ctx, conn, seeded)
	assertNewWritesWork(t, ctx, conn, seeded)
}

// seededIDs are the rows written before the upgrade.
type seededIDs struct {
	organization uuid.UUID
	project      uuid.UUID
	definition   uuid.UUID
	instance     uuid.UUID
	task         uuid.UUID
	form         uuid.UUID
	user         uuid.UUID
}

// ageTheDatabase turns a freshly migrated schema back into the shape an
// installation from before this release is in, and puts rows in it.
//
// Renaming the columns back rather than checking out the old models: the models
// carry the new names now, so the only way to produce the old shape from this
// tree is to undo the rename. That is exactly the shape migration 18 and 19
// look for, which is the point.
func ageTheDatabase(t *testing.T, db *gorm.DB) seededIDs {
	t.Helper()

	// The pre-release column names, from migrations 18 and 19.
	for _, rename := range []struct{ table, from, to string }{
		{"forms", "fields", "schema"},
		{"connectors", "properties", "schema"},
		{"external_tasks", "instance_id", "process_instance_id"},
		{"external_tasks", "definition_id", "process_definition_id"},
		{"user_organizations", "user_id", "user_model_id"},
		{"user_organizations", "organization_id", "organization_model_id"},
		{"user_projects", "user_id", "user_model_id"},
		{"user_projects", "project_id", "project_model_id"},
	} {
		if !db.Migrator().HasTable(rename.table) || !db.Migrator().HasColumn(rename.table, rename.from) {
			continue
		}
		if err := db.Exec("ALTER TABLE " + quoted(rename.table) + " RENAME COLUMN " +
			quoted(rename.from) + " TO " + quoted(rename.to)).Error; err != nil {
			t.Fatalf("age %s.%s: %v", rename.table, rename.from, err)
		}
	}

	// GORM fills identifiers and timestamps in Go, so a table it created has no
	// database default on any of them. Dropping them here is what makes the
	// reconciliation step something this test can fail on rather than assume.
	for _, table := range []string{"organizations", "projects", "process_definitions", "process_instances", "tasks", "forms", "users"} {
		for _, column := range []string{"id", "created_at", "updated_at"} {
			if !db.Migrator().HasColumn(table, column) {
				continue
			}
			if err := db.Exec("ALTER TABLE " + quoted(table) + " ALTER COLUMN " +
				quoted(column) + " DROP DEFAULT").Error; err != nil {
				t.Fatalf("drop the default on %s.%s: %v", table, column, err)
			}
		}
	}

	// The storm-only tables did not exist before this release.
	for _, table := range []string{
		"workflow_users", "workflow_groups", "workflow_group_memberships",
		"participant_sources", "platform_users", "platform_roles", "platform_role_assignments",
	} {
		if err := db.Exec("DROP TABLE IF EXISTS " + quoted(table) + " CASCADE").Error; err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}

	// Nor did a membership's own roles, which migration 30 adds. The account's
	// membership below is read back through the repository, which reads them.
	if err := db.Exec(`ALTER TABLE user_organizations DROP COLUMN IF EXISTS roles`).Error; err != nil {
		t.Fatalf("drop user_organizations.roles: %v", err)
	}

	return seedRows(t, db)
}

// seedRows writes the rows an existing installation would already hold, through
// GORM — the only writer that existed before the upgrade.
func seedRows(t *testing.T, db *gorm.DB) seededIDs {
	t.Helper()
	ids := seededIDs{
		organization: uuid.Must(uuid.NewV7()),
		project:      uuid.Must(uuid.NewV7()),
		definition:   uuid.Must(uuid.NewV7()),
		instance:     uuid.Must(uuid.NewV7()),
		task:         uuid.Must(uuid.NewV7()),
		form:         uuid.Must(uuid.NewV7()),
		user:         uuid.Must(uuid.NewV7()),
	}
	now := time.Now().UTC()
	base := func(id uuid.UUID) models.Base {
		return models.Base{ID: models.FromUUID(id), CreatedAt: now, UpdatedAt: now}
	}

	for _, row := range []any{
		&models.OrganizationModel{Base: base(ids.organization), Name: "Legacy Org"},
		&models.ProjectModel{
			Base: base(ids.project), OrganizationID: models.FromUUID(ids.organization), Name: "Legacy Project",
		},
		&models.UserModel{
			Base: base(ids.user), Username: "legacy-admin", FullName: "Legacy Admin",
			Roles: []string{entities.RoleAdmin},
		},
		&models.ProcessDefinitionModel{
			Base: base(ids.definition), ProjectID: models.FromUUID(ids.project),
			Key: "legacy-approval", Name: "Legacy approval", Version: 1,
		},
		&models.ProcessInstanceModel{
			Base: base(ids.instance), ProjectID: models.FromUUID(ids.project),
			DefinitionID: models.FromUUID(ids.definition), Status: models.ProcessActive,
			Variables: models.EncryptedMap{"amount": float64(42)},
		},
		&models.TaskModel{
			Base: base(ids.task), ProjectID: models.FromUUID(ids.project),
			InstanceID: models.FromUUID(ids.instance), NodeID: "approve", Name: "Approve",
			Status: models.TaskUnclaimed, Assignee: "legacy-admin",
			Variables: models.EncryptedMap{"amount": float64(42)},
		},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("seed a pre-upgrade row: %v", err)
		}
	}

	// The form is written with the old column name, which is the whole reason
	// migration 18 exists — GORM cannot write it once the model says `fields`.
	if err := db.Exec(
		`INSERT INTO forms (id, created_at, updated_at, project_id, "key", name, "schema")
		 VALUES ($1, $2, $2, $3, 'legacy-form', 'Legacy form', '{"fields":[{"id":"amount"}]}')`,
		models.FromUUID(ids.form), now, models.FromUUID(ids.project)).Error; err != nil {
		t.Fatalf("seed a pre-upgrade form: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO user_organizations (user_model_id, organization_model_id) VALUES ($1, $2)`,
		models.FromUUID(ids.user), models.FromUUID(ids.organization)).Error; err != nil {
		t.Fatalf("seed a pre-upgrade membership: %v", err)
	}
	return ids
}

// assertTheOldRowsSurvived reads what was written before the upgrade through
// the repositories that were rewritten.
//
// This is the assertion that matters. A migration that runs cleanly and leaves
// a column the new code cannot read is indistinguishable from a successful
// upgrade until somebody opens the page.
func assertTheOldRowsSurvived(t *testing.T, ctx context.Context, conn *stormdb.Conn, seeded seededIDs) {
	t.Helper()
	repo := repositories.NewRepository(conn)

	project, err := repo.Project().Get(ctx, seeded.project)
	if err != nil {
		t.Fatalf("a project written before the upgrade is unreadable after it: %v", err)
	}
	if project.Name != "Legacy Project" {
		t.Errorf("the project came back as %q", project.Name)
	}

	definition, err := repo.Definition().Get(ctx, seeded.definition)
	if err != nil {
		t.Fatalf("a definition written before the upgrade is unreadable after it: %v", err)
	}
	if definition.Version != 1 {
		t.Errorf("the definition came back as version %d", definition.Version)
	}

	// The encrypted columns: ciphertext written by GORM's EncryptedMap has to
	// decrypt through storm's read path, or every running instance loses its
	// business data on upgrade.
	instance, err := repo.Process().Get(ctx, seeded.instance)
	if err != nil {
		t.Fatalf("an instance written before the upgrade is unreadable after it: %v", err)
	}
	if instance.Variables["amount"] != float64(42) {
		t.Errorf("the instance's variables came back as %v; the encryption did not survive the upgrade", instance.Variables)
	}

	task, err := repo.Task().Get(ctx, seeded.task)
	if err != nil {
		t.Fatalf("a task written before the upgrade is unreadable after it: %v", err)
	}
	if task.Variables["amount"] != float64(42) {
		t.Errorf("the task's variables came back as %v", task.Variables)
	}

	// The renamed column. Reading this proves migration 18 ran and that the
	// repository names what the migration left behind.
	form, err := repo.Form().Get(ctx, seeded.form)
	if err != nil {
		t.Fatalf("a form written under the old column name is unreadable after the rename: %v", err)
	}
	if form.Key != "legacy-form" {
		t.Errorf("the form came back as %q", form.Key)
	}
	// The moved column's *contents*, not just its name. A rename that skipped
	// leaves the new column empty and every read succeeds — the form is there,
	// with no definition in it, and nothing anywhere says so.
	if form.Schema == nil {
		t.Fatal("the form's definition did not survive the column rename; the upgrade lost it silently")
	}
	if _, ok := form.Schema["fields"]; !ok {
		t.Errorf("the form's definition came back as %v", form.Schema)
	}

	// The renamed join column, which is how an account still belongs to its
	// tenant after the upgrade — and therefore whether anybody can still sign in
	// and see anything.
	user, err := repo.User().Get(ctx, seeded.user)
	if err != nil {
		t.Fatalf("an account written before the upgrade is unreadable after it: %v", err)
	}
	if len(user.Organizations) != 1 || uuid.UUID(user.Organizations[0].ID) != seeded.organization {
		t.Fatalf("the account lost its organization in the upgrade: %v", user.Organizations)
	}
}

// assertNewWritesWork covers the other half: an upgraded database has to accept
// writes, not only answer reads.
//
// Separately from the reads because they fail differently. A missing column
// default is invisible to every read and panics the first insert.
func assertNewWritesWork(t *testing.T, ctx context.Context, conn *stormdb.Conn, seeded seededIDs) {
	t.Helper()
	repo := repositories.NewRepository(conn)

	// No id and no timestamps: the database supplies them, which is what the
	// reconciled defaults are for.
	if err := repo.Form().Create(ctx, models.FormModel{
		ProjectID: models.FromUUID(seeded.project),
		Key:       "written-after-the-upgrade",
		Name:      "After",
		Schema:    map[string]any{"fields": []any{}},
	}); err != nil {
		t.Fatalf("an upgraded database refused a new form: %v", err)
	}

	if _, err := repo.Process().Create(ctx, models.ProcessInstanceModel{
		ProjectID:    models.FromUUID(seeded.project),
		DefinitionID: models.FromUUID(seeded.definition),
		Status:       models.ProcessActive,
		Variables:    models.EncryptedMap{"amount": float64(7)},
	}); err != nil {
		t.Fatalf("an upgraded database refused a new process instance: %v", err)
	}

	// A storm-only table, which did not exist before the upgrade at all. Built
	// directly because the facade does not carry it — the participant directory
	// is wired at the composition root, not through the repository set.
	if _, err := pg.NewWorkflowUserRepository(conn).Upsert(ctx, entities.WorkflowUser{
		Project:  &entities.Project{ID: seeded.project},
		Username: "after-the-upgrade",
		Active:   true,
	}); err != nil {
		t.Fatalf("an upgraded database refused a participant: %v", err)
	}
}

// breaking reports whether a drift statement would break a query rather than
// cost an index scan. Type differences are known debt; see tests/drift.
func breaking(statement string) bool {
	return contains(statement, "ADD COLUMN") || contains(statement, "DROP COLUMN")
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func quoted(identifier string) string { return `"` + identifier + `"` }
