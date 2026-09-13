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
		{
			Version: 13,
			Name:    "index the job claim query",
			// The job worker asks for pending jobs whose time has come, oldest
			// first, several times a second forever. The table has single-column
			// indexes on status and next_run_at, which is not the same thing: the
			// planner picks one and filters the rest, so the scan grows with every
			// completed job ever written. A composite matching the predicate and
			// the order keeps it flat.
			Run: func(_ context.Context, db *gorm.DB) error {
				model, err := modelForTable(db, models, "jobs")
				if err != nil {
					return err
				}
				const index = "ix_jobs_claim"
				if db.Migrator().HasIndex(model, index) {
					return nil
				}
				if err := db.Exec(
					"CREATE INDEX " + index + " ON jobs (status, next_run_at)",
				).Error; err != nil {
					return fmt.Errorf("create %s: %w", index, err)
				}
				return nil
			},
		},
		{
			Version: 12,
			Name:    "signed webhook receiver tables",
			// The signed-webhook feature shipped its models and repository but
			// no migration, so `webhooks` and `webhook_deliveries` existed only
			// on installations old enough to predate versioning (where the
			// baseline AutoMigrate had made them). Every newer install answered
			// the feature's endpoints with "no such table" and a 500, while
			// /readyz stayed green because it only pings the database — a broken
			// feature that never paged anyone.
			Run: func(_ context.Context, db *gorm.DB) error {
				for _, table := range []string{"webhooks", "webhook_deliveries"} {
					model, err := modelForTable(db, models, table)
					if err != nil {
						return err
					}
					if db.Migrator().HasTable(model) {
						continue
					}
					if err := db.AutoMigrate(model); err != nil {
						return fmt.Errorf("create %s: %w", table, err)
					}
				}
				return nil
			},
		},
		{
			Version: 14,
			Name:    "which version of a process is live",
			// Deploying a process used to promote it in the same act that saved it,
			// because "the live version" was defined as whichever sorted highest.
			// This table makes the choice explicit, and reversible.
			//
			// Deliberately not backfilled. An installation that has never promoted
			// anything has no rows here, and the reader treats an absent row as "the
			// highest version" — which is exactly what it did before. The first
			// deploy or promotion after the upgrade writes the row.
			Run: func(_ context.Context, db *gorm.DB) error {
				model, err := modelForTable(db, models, "process_definition_releases")
				if err != nil {
					return err
				}
				if db.Migrator().HasTable(model) {
					return nil
				}
				if err := db.AutoMigrate(model); err != nil {
					return fmt.Errorf("create process_definition_releases: %w", err)
				}
				return nil
			},
		},
		{
			Version: 16,
			Name:    "the runtimes a project deploys into",
			// A project can have a development, staging and production runtime,
			// each with its own database and its own port. This table is the
			// registry of them; the data they hold is in the databases they name,
			// not here.
			Run: func(_ context.Context, db *gorm.DB) error {
				model, err := modelForTable(db, models, "environments")
				if err != nil {
					return err
				}
				if db.Migrator().HasTable(model) {
					return nil
				}
				if err := db.AutoMigrate(model); err != nil {
					return fmt.Errorf("create environments: %w", err)
				}
				return nil
			},
		},
		{
			Version: 15,
			Name:    "releases are a timeline, not a single current version",
			// A release row said only "this version is live". Arranging a cutover
			// in advance needs it to say "live from this moment", so the answer
			// becomes a function of the rows and the clock and nothing has to
			// wake up to apply it.
			//
			// Written to be safe in both directions: on a fresh database
			// migration 14 already created the table from the current model, so
			// the column and the new index are there and every step below is a
			// no-op. On a database that ran 14 in its first form, this adds them.
			Run: func(_ context.Context, db *gorm.DB) error {
				model, err := modelForTable(db, models, "process_definition_releases")
				if err != nil {
					return err
				}
				if !db.Migrator().HasColumn(model, "activate_at") {
					if err := db.Migrator().AddColumn(model, "activate_at"); err != nil {
						return fmt.Errorf("add process_definition_releases.activate_at: %w", err)
					}
				}
				// Rows written before the column existed are already in force, so
				// they take the time they were created. Guarded on IS NULL so it
				// does nothing when the dialect gave the new column a default.
				if err := db.Model(model).
					Where("activate_at IS NULL").
					UpdateColumn("activate_at", gorm.Expr("created_at")).Error; err != nil {
					return fmt.Errorf("backfill process_definition_releases.activate_at: %w", err)
				}
				// The old index made (key, project) unique, which a timeline must
				// not be — the same key changes version more than once.
				const oldIndex = "idx_definition_release_key"
				if db.Migrator().HasIndex(model, oldIndex) {
					// Raw SQL rather than Migrator().DropIndex.
					//
					// GORM's PostgreSQL migrator builds `DROP INDEX
					// CURRENT_SCHEMA.<name>` and PostgreSQL refuses it —
					// CURRENT_SCHEMA is a function, not an identifier, so the
					// statement is a syntax error every time, on any search_path.
					// This migration is guarded by HasIndex, so only an
					// installation upgrading from before it ever reached the
					// call: a fresh install skips it and looks fine, which is
					// why nothing caught it until the suite was pointed at a
					// real PostgreSQL.
					//
					// IF EXISTS rather than relying on the guard alone, because
					// two replicas can both pass HasIndex and only one can drop.
					if err := db.Exec("DROP INDEX IF EXISTS " + oldIndex).Error; err != nil {
						return fmt.Errorf("drop %s: %w", oldIndex, err)
					}
				}
				const newIndex = "idx_definition_release_timeline"
				if !db.Migrator().HasIndex(model, newIndex) {
					if err := db.Migrator().CreateIndex(model, newIndex); err != nil {
						return fmt.Errorf("create %s: %w", newIndex, err)
					}
				}
				return nil
			},
		},
		{
			Version: 17,
			Name:    "an SSE event says who it is for",
			// The live event stream carries process variables. The bus that
			// moves events between replicas carried only the payload, so a
			// replica reading one back had no way to know whose it was and
			// delivered it to every browser it held — every tenant's business
			// data, live, to anybody signed in.
			//
			// These two columns are the audience. Rows written before them are
			// left null and dropped on delivery rather than broadcast: the bus
			// prunes within minutes, and an event whose audience is unknown has
			// no safe audience.
			Run: func(_ context.Context, db *gorm.DB) error {
				model, err := modelForTable(db, models, "broadcast_events")
				if err != nil {
					return err
				}
				for _, column := range []string{"organization_id", "environment_id"} {
					if db.Migrator().HasColumn(model, column) {
						continue
					}
					if err := db.Migrator().AddColumn(model, column); err != nil {
						return fmt.Errorf("add broadcast_events.%s: %w", column, err)
					}
				}
				const index = "ix_broadcast_events_org"
				if !db.Migrator().HasIndex(model, index) {
					if err := db.Migrator().CreateIndex(model, index); err != nil {
						return fmt.Errorf("create %s: %w", index, err)
					}
				}
				return nil
			},
		},
		{
			Version: 18,
			Name:    "one name per column",
			// Four columns whose Go field and database column disagreed, found
			// by diffing the GORM schema against the storm model rather than by
			// a query failing. Nothing broke while only GORM read them; the
			// moment a ported repository named the column the model describes,
			// it was a SQL error on every read of that table.
			//
			// Not a plain rename, and the reason is the migration that runs
			// first. The baseline is AutoMigrate over the *current* models, so
			// by the time this executes on an existing installation the new
			// column already exists — added, empty, beside the old one holding
			// the data. A rename guarded on "skip if the new column is there"
			// therefore skipped on every real upgrade and left every form
			// without its definition, every connector without its properties
			// and every external task without its references. Silently: the
			// migration reported success.
			//
			// So all three states are handled. Only the old column is a rename,
			// which is metadata-only in PostgreSQL. Both is a copy and a drop.
			// Only the new is a fresh install with nothing to do.
			Run: func(_ context.Context, db *gorm.DB) error {
				renames := []struct{ table, from, to string }{
					{"forms", "schema", "fields"},
					{"connectors", "schema", "properties"},
					{"external_tasks", "process_instance_id", "instance_id"},
					{"external_tasks", "process_definition_id", "definition_id"},
				}
				for _, rename := range renames {
					if err := moveColumn(db, models, rename.table, rename.from, rename.to); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			Version: 19,
			Name:    "the join tables name their columns",
			// GORM derived a many-to-many join table's columns from the Go type
			// names, so an account's organizations were joined on
			// user_model_id and organization_model_id — names that say nothing
			// and match no other foreign key in the database.
			//
			// RENAME is metadata-only in PostgreSQL. Guarded both ways so a
			// fresh installation, whose AutoMigrate already created the right
			// names, does not fail on a rename with nothing to rename.
			Run: func(_ context.Context, db *gorm.DB) error {
				renames := []struct{ table, from, to string }{
					{"user_organizations", "user_model_id", "user_id"},
					{"user_organizations", "organization_model_id", "organization_id"},
					{"user_projects", "user_model_id", "user_id"},
					{"user_projects", "project_model_id", "project_id"},
				}
				for _, rename := range renames {
					if err := moveColumn(db, models, rename.table, rename.from, rename.to); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			Version: 20,
			Name:    "index the instance list query",
			// The instance list can now filter by state in the database rather
			// than in the browser, which is the only way a project with 500,000
			// instances can surface the twelve that failed. Doing it over the
			// single-column indexes the models declare would be a poor trade:
			// the planner picks one of project_id or status, filters the rest,
			// and then sorts everything it kept — so the page someone looks at
			// most often gets slower the longer the installation runs.
			//
			// Two composites, matching the two shapes the list actually issues.
			// Both lead with project_id because tenant scoping is never absent,
			// and both end with created_at DESC because that is the order every
			// one of them asks for: with equality on the leading columns the
			// index returns the rows already sorted, so a page is a walk of
			// twenty-five entries rather than a sort of the project.
			//
			// Built CONCURRENTLY, which is the whole reason this migration is
			// more than two lines. A plain CREATE INDEX takes a SHARE lock, and
			// that blocks every INSERT and UPDATE on process_instances until it
			// finishes — on the busiest table in the system, during a rolling
			// deploy where the old replica is still accepting work. Starting a
			// process would hang for as long as the build took.
			Run: func(ctx context.Context, db *gorm.DB) error {
				indexes := []struct{ name, columns string }{
					// The default view: every state, newest first.
					{"ix_process_instances_project_created", "project_id, created_at DESC"},
					// With a state chosen, and with a definition chosen on top of
					// one — definition_id is filtered from the rows this returns,
					// which stays cheap because the walk stops at the page size.
					{"ix_process_instances_project_status", "project_id, status, created_at DESC"},
				}
				for _, index := range indexes {
					if err := createIndexConcurrently(ctx, db,
						"process_instances", index.name, index.columns); err != nil {
						return err
					}
				}
				return nil
			},
		},
	}
}

// createIndexConcurrently builds an index without locking out writers.
//
// CONCURRENTLY is what makes an index on a large, busy table safe to add during
// an upgrade, and it comes with two sharp edges this handles:
//
//   - **It cannot run inside a transaction.** Migrations that leave
//     Transactional off execute their statements directly, which is why this is
//     usable here at all. A migration that needed a transaction could not use it.
//   - **A failed build leaves an INVALID index behind.** PostgreSQL keeps the
//     catalog row, so every "does it exist?" guard says yes for ever while the
//     planner refuses to use it — a permanently missing index that reports
//     itself as present. So the guard asks whether it is *valid*, and drops the
//     wreckage of a previous attempt before trying again.
//
// IF NOT EXISTS on top of the guard, because two replicas can both read "no
// index" and only one of them can create it.
//
// PostgreSQL is the only engine this product ships on, but not the only one it
// is migrated against: the completeness tests run the whole list over an
// in-memory SQLite database precisely so they owe nothing to a test harness.
// CONCURRENTLY and pg_index are both PostgreSQL-only, so everywhere else this
// falls back to a plain create — correct there, and the locking concern that
// makes CONCURRENTLY necessary does not exist on a fresh in-memory database.
func createIndexConcurrently(ctx context.Context, db *gorm.DB, table, name, columns string) error {
	if db.Name() != "postgres" {
		if err := db.WithContext(ctx).Exec(fmt.Sprintf(
			"CREATE INDEX IF NOT EXISTS %s ON %s (%s)", name, table, columns,
		)).Error; err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
		return nil
	}

	valid, err := indexIsValid(ctx, db, name)
	if err != nil {
		return err
	}
	if valid {
		return nil
	}
	// Present but invalid: an earlier attempt died part-way. Dropping is the
	// only way forward — CREATE INDEX IF NOT EXISTS would see the broken one and
	// do nothing.
	if err := db.WithContext(ctx).
		Exec("DROP INDEX CONCURRENTLY IF EXISTS " + name).Error; err != nil {
		return fmt.Errorf("drop the invalid %s: %w", name, err)
	}
	if err := db.WithContext(ctx).Exec(fmt.Sprintf(
		"CREATE INDEX CONCURRENTLY IF NOT EXISTS %s ON %s (%s)", name, table, columns,
	)).Error; err != nil {
		return fmt.Errorf("create %s: %w", name, err)
	}
	// Built, but a concurrent build can still finish invalid — it gives up
	// rather than failing loudly when it cannot see a consistent snapshot. Left
	// unchecked that is the silent missing index again.
	valid, err = indexIsValid(ctx, db, name)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("%s was built but is not valid; it must be rebuilt before the planner will use it", name)
	}
	return nil
}

// indexIsValid reports whether an index exists and is usable by the planner.
func indexIsValid(ctx context.Context, db *gorm.DB, name string) (bool, error) {
	var valid bool
	err := db.WithContext(ctx).Raw(`
		SELECT i.indisvalid
		  FROM pg_class c
		  JOIN pg_index i ON i.indexrelid = c.oid
		 WHERE c.relname = ?
		   AND c.relnamespace = current_schema()::regnamespace`, name).Scan(&valid).Error
	if err != nil {
		return false, fmt.Errorf("check whether %s is valid: %w", name, err)
	}
	return valid, nil
}

// moveColumn gets a column's data under its new name, whatever state the table
// is in.
//
// Three states, because the baseline migration is AutoMigrate over the current
// models and therefore adds the new column before any rename can run:
//
//   - only the old column: RENAME, which is metadata-only in PostgreSQL — no
//     table rewrite and no long lock.
//   - both: the new one is empty and the old one holds the data, so copy across
//     and drop the old. This is the state every existing installation is in,
//     and the state a rename-only migration silently skipped.
//   - only the new column: a fresh install, nothing to do.
//
// The copy fills only rows whose new value is still null, so running twice
// cannot overwrite anything written between the two runs.
func moveColumn(db *gorm.DB, models []any, table, from, to string) error {
	if !db.Migrator().HasTable(table) {
		return nil
	}
	hasOld, hasNew := db.Migrator().HasColumn(table, from), db.Migrator().HasColumn(table, to)
	switch {
	case !hasOld:
		return nil
	case !hasNew:
		if err := db.Exec(fmt.Sprintf("ALTER TABLE %q RENAME COLUMN %q TO %q", table, from, to)).Error; err != nil {
			return fmt.Errorf("rename %s.%s to %s: %w", table, from, to, err)
		}
		return nil
	}

	if err := db.Exec(fmt.Sprintf(
		"UPDATE %q SET %q = %q WHERE %q IS NULL AND %q IS NOT NULL",
		table, to, from, to, from)).Error; err != nil {
		return fmt.Errorf("copy %s.%s into %s: %w", table, from, to, err)
	}
	// Dropped rather than left behind: two columns holding one fact is how the
	// next reader picks the wrong one, and the old name is what the drift check
	// would go on reporting for ever.
	if err := db.Exec(fmt.Sprintf("ALTER TABLE %q DROP COLUMN %q", table, from)).Error; err != nil {
		return fmt.Errorf("drop %s.%s: %w", table, from, err)
	}
	_ = models
	return nil
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
