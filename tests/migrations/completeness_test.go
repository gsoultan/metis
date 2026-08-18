package migrations_test

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gsoultan/gobpm/server/repositories/gorms"
	"github.com/gsoultan/gobpm/server/repositories/migrations"
	"github.com/gsoultan/gobpm/server/repositories/models"
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
