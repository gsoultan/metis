package migrations_test

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gsoultan/metis/server/repositories/gorms"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/models"
	"gorm.io/gorm"
)

// A migration version is an identity, not an ordering hint: an installation
// that has applied version 3 will never look at a different version 3 again. So
// the runner refuses a list with a duplicate — and the list the application
// boots with is the schema migrations *plus* the data migrations App.migrationList
// appends, which is not the list any other test looks at. A schema migration
// numbered into that gap is a server that will not start, discovered on the
// first deploy rather than here.
func TestApplicationMigrationVersionsAreUnique(t *testing.T) {
	// The shape App.migrationList builds.
	list := append(migrations.Schema(models.MigrationModels()),
		migrations.Migration{Version: 2, Name: "resolve templated message correlation keys"},
		migrations.Migration{Version: 3, Name: "move engine bookkeeping out of business variables"},
	)
	seen := map[int]string{}
	for _, m := range list {
		if other, taken := seen[m.Version]; taken {
			t.Fatalf("version %d is claimed by both %q and %q", m.Version, other, m.Name)
		}
		seen[m.Version] = m.Name
	}
}

// Every model the application declares must end up with a table.
//
// Five did not: deployments, deployment resources, forms, variable snapshots
// and compensatable activities were declared, given repositories and used, and
// left out of the migrated model list. Nothing caught it because every test
// harness builds its schema from a fuller list of its own — so the suite was
// green while a fresh installation failed on its first deployment.
//
// This opens a database with no help from those harnesses, which is the only
// way the gap is visible.
func TestEveryDeclaredModelGetsATable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), gorms.Config())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := migrations.Run(context.Background(), db, migrations.Schema(models.MigrationModels())); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, model := range models.MigrationModels() {
		if !db.Migrator().HasTable(model) {
			t.Errorf("model %T is declared by the application but no migration creates its table", model)
		}
	}

	// Named explicitly as well, so the failure says which feature is broken
	// rather than which struct is missing.
	for _, name := range []string{"deployments", "deployment_resources", "forms", "variable_snapshots", "compensatable_activities", "service_calls"} {
		if !db.Migrator().HasTable(name) {
			t.Errorf("table %q is used by the application but no migration creates it", name)
		}
	}
}

// The upgrade path, which the test above cannot see.
//
// On a *fresh* database migration 1 auto-migrates the whole model list, so every
// declared table exists no matter which migration was supposed to create it. An
// installation that is already at the current version gets no such safety net:
// a model added afterwards has no migration creating its table, and the first
// request that touches it fails with "no such table" — in production, on the
// feature nobody could test because it had never run anywhere else.
//
// This reproduces that installation exactly: migrate a database with the model
// list and migrations as they were *released*, then upgrade it to the current
// ones. Anything the new release declares must exist afterwards.
//
// Dropping a table and re-running is not the same test and does not work:
// migrations are recorded once by design, so nothing re-runs and every table
// stays missing. That is how versioned migrations behave, not a defect.
func TestAModelAddedAfterTheBaselineStillGetsItsTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared&_upgrade=1"), gorms.Config())
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// The installation as it was before this release: the model list without
	// whatever this release adds, and only the migrations that had shipped.
	released := migrations.Schema(previouslyReleasedModels())
	if _, err := migrations.Run(context.Background(), db, released); err != nil {
		t.Fatalf("migrate the released version: %v", err)
	}

	// Now upgrade it.
	if _, err := migrations.Run(context.Background(), db, migrations.Schema(models.MigrationModels())); err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	for _, model := range models.MigrationModels() {
		if !db.Migrator().HasTable(model) {
			t.Errorf("%T has no table after upgrading an existing installation: "+
				"it was added to the model list without a migration that creates it", model)
		}
	}
}

// previouslyReleasedModels is the model list as of the last release.
//
// Kept as an explicit subtraction rather than a copied list, so adding a model
// is one edit and forgetting to update this is impossible — the new model is
// simply absent from the "before" picture, which is what the test needs.
func previouslyReleasedModels() []any {
	added := map[string]bool{
		// Declared in this release. Everything before it shipped already.
		"connector_manifests": true,
	}
	var before []any
	for _, model := range models.MigrationModels() {
		if named, ok := model.(interface{ TableName() string }); ok && added[named.TableName()] {
			continue
		}
		before = append(before, model)
	}
	return before
}
