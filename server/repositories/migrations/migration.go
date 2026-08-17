// Package migrations applies ordered, recorded changes to the database.
//
// It replaces calling AutoMigrate on every boot. AutoMigrate is a fine way to
// create a schema and a poor way to evolve one: it keeps no history, so nobody
// can tell which version a database is at; it offers no review step, so a model
// field renamed in a pull request becomes a live DDL statement against customer
// data the moment that build starts; and it has no rollback. It also never drops
// or narrows anything, so a schema that has drifted stays drifted in silence.
//
// Migrations are written in Go rather than SQL because this product supports
// SQLite, MySQL, PostgreSQL and SQL Server. SQL files would mean maintaining
// four copies of every change and discovering the differences in production;
// GORM's migrator resolves dialect differences for us.
package migrations

import (
	"context"

	"gorm.io/gorm"
)

// Migration is one ordered, recorded change to the database.
//
// Version must be unique and must never be reused or renumbered once released:
// it is the identity recorded in schema_migrations, and an installation that has
// already applied version 3 will never look at a different version 3 again.
type Migration struct {
	Version int
	Name    string

	// Transactional asks the runner to wrap Run in a transaction.
	//
	// Set it for data migrations, where a partial result is a corrupt result.
	// Leave it off for schema changes: MySQL and SQL Server commit DDL
	// implicitly, so a transaction there buys nothing and only disguises how
	// much rollback protection a migration really has.
	Transactional bool

	Run func(ctx context.Context, db *gorm.DB) error
}

// Schema returns the migrations that depend on nothing but the database.
//
// Data migrations that need the repository or service layer are supplied by the
// caller instead — see App.migrate. Repositories must not import services, and a
// migration list that reached upward for them would invert the layering the rest
// of the codebase keeps.
func Schema(models []any) []Migration {
	return []Migration{
		{
			Version: 1,
			Name:    "baseline schema",
			// The baseline is deliberately AutoMigrate over the model list: it
			// reproduces exactly the schema every existing installation already
			// has, so this migration is a no-op on all of them and creates
			// everything on a fresh database. That is what lets versioning start
			// without a migration that has to guess at the current state.
			//
			// Changes after this one are explicit. A new model field will no
			// longer appear by itself, which is the point — see DriftReport for
			// the guard rail that says so out loud during development.
			Run: func(_ context.Context, db *gorm.DB) error {
				return db.AutoMigrate(models...)
			},
		},
	}
}
