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
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
		{
			// 2 and 3 are the data migrations App.migrationList appends. A
			// schema migration cannot take a number a shipped migration already
			// has: the runner refuses the whole list, and the server does not
			// start.
			Version: 4,
			Name:    "unique version per definition key",
			// Deliberately not transactional: the DDL half commits implicitly on
			// MySQL and SQL Server anyway, so a transaction here would only
			// disguise how much of this is recoverable. The repair half is
			// idempotent — re-running it on an already-repaired table changes
			// nothing — so an interrupted run is safe to retry.
			Run: func(ctx context.Context, db *gorm.DB) error {
				for _, table := range versionedDefinitionTables {
					model, err := modelForTable(db, models, table.name)
					if err != nil {
						return err
					}
					if err := renumberDuplicateVersions(ctx, db, model, table.name); err != nil {
						return err
					}
					if err := createUniqueVersionIndex(db, model, table.name, table.index); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			Version: 5,
			Name:    "create the tables the baseline list omitted",
			// Five models — deployments, deployment resources, forms, variable
			// snapshots and compensatable activities — were declared, given
			// repositories and used, but left out of the model list the baseline
			// migrates. Every test harness built its schema from a fuller list of
			// its own, so nothing noticed: on a fresh installation the first
			// deployment failed with "no such table".
			//
			// AutoMigrate over the corrected list rather than five explicit
			// creates: it is a no-op for the tables that already exist, which is
			// what makes this safe to run against an installation that somehow
			// has them.
			Run: func(_ context.Context, db *gorm.DB) error {
				return db.AutoMigrate(models...)
			},
		},
		{
			Version: 6,
			Name:    "decision tables carry their examples",
			// A decision table nobody can test is a spreadsheet with extra
			// steps, so the examples it is expected to get right are stored
			// beside it. One column, added explicitly rather than by running
			// AutoMigrate again: a migration that says which change it makes is
			// one somebody can review.
			Run: func(_ context.Context, db *gorm.DB) error {
				model, err := modelForTable(db, models, "decision_definitions")
				if err != nil {
					return err
				}
				if db.Migrator().HasColumn(model, "tests") {
					return nil
				}
				if err := db.Migrator().AddColumn(model, "Tests"); err != nil {
					return fmt.Errorf("add decision_definitions.tests: %w", err)
				}
				return nil
			},
		},
		{
			Version: 7,
			Name:    "connectors described by a document",
			// A connector can now be a manifest rather than a Go function, and
			// the manifests have to survive a restart — an in-memory registry
			// means a connector installed on one replica is unknown to the
			// others and gone on the next deploy.
			Run: func(_ context.Context, db *gorm.DB) error {
				model, err := modelForTable(db, models, "connector_manifests")
				if err != nil {
					return err
				}
				if db.Migrator().HasTable(model) {
					return nil
				}
				if err := db.AutoMigrate(model); err != nil {
					return fmt.Errorf("create connector_manifests: %w", err)
				}
				return nil
			},
		},
		{
			Version: 8,
			Name:    "idempotency records survive the process",
			// The Idempotency-Key cache lived in the serving process, so a
			// client retry that reached a different replica found nothing and
			// executed the write a second time. For an engine that moves money
			// that is a duplicate business action, which is what the header
			// exists to prevent.
			Run: func(_ context.Context, db *gorm.DB) error {
				model, err := modelForTable(db, models, "idempotency_records")
				if err != nil {
					return err
				}
				if db.Migrator().HasTable(model) {
					return nil
				}
				if err := db.AutoMigrate(model); err != nil {
					return fmt.Errorf("create idempotency_records: %w", err)
				}
				return nil
			},
		},
		{
			Version: 9,
			Name:    "SSE events reach browsers on other replicas",
			// The SSE client registry holds open response writers, so it is
			// necessarily per-process. That meant a browser connected to
			// replica A never saw anything that happened on replica B: its
			// lists stopped updating, silently, with no error anywhere. This
			// table is the bus that carries an event between replicas.
			Run: func(_ context.Context, db *gorm.DB) error {
				model, err := modelForTable(db, models, "broadcast_events")
				if err != nil {
					return err
				}
				if db.Migrator().HasTable(model) {
					return nil
				}
				if err := db.AutoMigrate(model); err != nil {
					return fmt.Errorf("create broadcast_events: %w", err)
				}
				return nil
			},
		},
		{
			Version: 10,
			Name:    "a password change ends existing sessions",
			// Changing a password stopped the old password working and left
			// every token minted with it valid for the rest of its 24-hour
			// life. Somebody changing their password because they believe they
			// are compromised is doing it to end the attacker's access.
			//
			// The column is left NULL for existing rows on purpose: filling it
			// with the migration time would sign every user on the
			// installation out at the moment of an upgrade, which is a
			// self-inflicted outage in the name of a fix.
			Run: func(_ context.Context, db *gorm.DB) error {
				model, err := modelForTable(db, models, "users")
				if err != nil {
					return err
				}
				if db.Migrator().HasColumn(model, "tokens_valid_from") {
					return nil
				}
				if err := db.Migrator().AddColumn(model, "tokens_valid_from"); err != nil {
					return fmt.Errorf("add users.tokens_valid_from: %w", err)
				}
				return nil
			},
		},
		{
			Version: 11,
			Name:    "rate limits and breakers are shared across replicas",
			// They were per-process, so N replicas applied each limit N times
			// over — a partner's quota exceeded N-fold, and N breakers each
			// deciding on their own whether a downstream was healthy.
			Run: func(_ context.Context, db *gorm.DB) error {
				model, err := modelForTable(db, models, "shared_counters")
				if err != nil {
					return err
				}
				if db.Migrator().HasTable(model) {
					return nil
				}
				if err := db.AutoMigrate(model); err != nil {
					return fmt.Errorf("create shared_counters: %w", err)
				}
				return nil
			},
		},
	}
}

// versionedDefinitionTables are the tables whose (project_id, key, version) must
// be unique. Named by table rather than by type because this package takes its
// models as an injected list — see the note on Schema.
var versionedDefinitionTables = []struct{ name, index string }{
	{name: "process_definitions", index: "ux_process_definitions_version"},
	{name: "decision_definitions", index: "ux_decision_definitions_version"},
}

// modelForTable resolves a table name back to the model behind it.
//
// The model is needed rather than the bare name because a query built from one
// quotes its columns — `key` is a reserved word on MySQL and a raw reference to
// it is rejected outright.
func modelForTable(db *gorm.DB, models []any, table string) (any, error) {
	for _, model := range models {
		statement, err := parseModel(db, model)
		if err != nil {
			return nil, err
		}
		if statement.Table == table {
			return model, nil
		}
	}
	return nil, fmt.Errorf("no model is registered for table %q", table)
}

// definitionVersionRow is the subset of a definition this migration reads.
//
// The `key` column needs quoting on MySQL, where it is a reserved word, so every
// reference to it goes through GORM's column handling rather than a raw string.
type definitionVersionRow struct {
	ID        string    `gorm:"column:id"`
	ProjectID string    `gorm:"column:project_id"`
	Key       string    `gorm:"column:key"`
	Version   int       `gorm:"column:version"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// renumberDuplicateVersions gives every row its own version number.
//
// An installation that ran the racy allocator may already hold two rows claiming
// the same version, and the unique index would refuse to build over them. The
// repair moves the later arrivals to fresh numbers rather than deleting them: a
// process definition is a business artifact, running instances point at it by
// ID, and a duplicate version is a labelling mistake, not a reason to destroy
// one of the two. The earliest row keeps the contested number, so whichever
// version a definition was deployed as first is the one that keeps its name.
//
// Soft-deleted rows are included. They still occupy a number as far as the
// unique index is concerned, and a version number that has been used once should
// never be reused — history that renumbers itself is not history.
func renumberDuplicateVersions(ctx context.Context, db *gorm.DB, model any, table string) error {
	var rows []definitionVersionRow
	err := db.WithContext(ctx).Unscoped().Model(model).
		Select([]string{"id", "project_id", "key", "version", "created_at"}).
		Order(clause.OrderBy{Columns: []clause.OrderByColumn{
			{Column: clause.Column{Name: "project_id"}},
			{Column: clause.Column{Name: "key"}},
			{Column: clause.Column{Name: "version"}},
			{Column: clause.Column{Name: "created_at"}},
			{Column: clause.Column{Name: "id"}},
		}}).
		Find(&rows).Error
	if err != nil {
		return fmt.Errorf("read %s versions: %w", table, err)
	}

	type series struct{ project, key string }

	// The highest number each series already uses, computed before anything is
	// assigned. Allocating as we go would hand a duplicate the next number up
	// from where we had read to — which a row further down the table already
	// holds, so the collision moves along the series instead of ending, and
	// versions nobody deployed twice get renumbered.
	highest := map[series]int{}
	for _, row := range rows {
		s := series{project: row.ProjectID, key: row.Key}
		if row.Version > highest[s] {
			highest[s] = row.Version
		}
	}

	taken := map[series]map[int]bool{}
	for _, row := range rows {
		s := series{project: row.ProjectID, key: row.Key}
		if taken[s] == nil {
			taken[s] = map[int]bool{}
		}
		if !taken[s][row.Version] {
			taken[s][row.Version] = true
			continue
		}

		highest[s]++
		next := highest[s]
		taken[s][next] = true

		// Table rather than Model so the repair does not touch updated_at, and
		// so soft-deleted rows are renumbered too — they hold a number as far as
		// the unique index is concerned.
		if err := db.WithContext(ctx).Table(table).
			Where("id = ?", row.ID).
			Update("version", next).Error; err != nil {
			return fmt.Errorf("renumber %s %s to version %d: %w", table, row.ID, next, err)
		}
	}
	return nil
}

// createUniqueVersionIndex adds the constraint, unless it is already there.
//
// A fresh database gets it from the baseline AutoMigrate, which reads the same
// tag on the model; an existing one has never seen it. Both paths run this
// migration, so it has to tolerate finding its own work already done.
func createUniqueVersionIndex(db *gorm.DB, model any, table, index string) error {
	if db.Migrator().HasIndex(model, index) {
		return nil
	}

	// Hand-written DDL rather than Migrator.CreateIndex, which reads the index
	// off a struct tag — and a tag would put this index in the baseline
	// AutoMigrate, which runs before the repair above. Quoting comes from the
	// dialector so `key`, a reserved word on MySQL, is spelled correctly on each
	// engine.
	statement, err := parseModel(db, model)
	if err != nil {
		return err
	}
	columns := make([]string, 0, len(versionSeriesColumns))
	for _, name := range versionSeriesColumns {
		columns = append(columns, statement.Quote(clause.Column{Name: name}))
	}

	ddl := fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)",
		statement.Quote(index), statement.Quote(table), strings.Join(columns, ", "))
	if err := db.Exec(ddl).Error; err != nil {
		return fmt.Errorf("create %s on %s: %w", index, table, err)
	}
	return nil
}

// versionSeriesColumns is what makes a definition's version unique: one series
// per key per project.
var versionSeriesColumns = []string{"project_id", "key", "version"}

// EnsureVersionIndexes adds the unique version constraints to a database built
// by AutoMigrate rather than by the migration runner.
//
// Test harnesses build their schema that way, and a constraint the tests do not
// have is a constraint the tests cannot check — the version allocator's whole
// behaviour under contention depends on this index existing.
func EnsureVersionIndexes(db *gorm.DB, models []any) error {
	for _, table := range versionedDefinitionTables {
		model, err := modelForTable(db, models, table.name)
		if err != nil {
			return err
		}
		if err := createUniqueVersionIndex(db, model, table.name, table.index); err != nil {
			return err
		}
	}
	return nil
}
