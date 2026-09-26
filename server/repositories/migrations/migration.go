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
		{
			Version: 21,
			Name:    "a task's priority is a number, not a maybe",
			// GET /api/v1/tasks panicked on a task whose priority was NULL:
			// `index out of range [7] with length 0`, from the generated
			// scanner reading eight bytes of an empty buffer. The inbox is the
			// page a business user lives in, and it did not answer slowly — it
			// dropped the connection.
			//
			// The model has always said `Priority int`, non-nullable. The
			// column was created by AutoMigrate, which does not carry that over,
			// so the schema permitted a value the reader cannot decode. Rows
			// written by the engine always carry one; rows written by anything
			// else — a bulk import, a data migration, or AutoMigrate adding the
			// column to a table that already had tasks in it — do not. That last
			// one is the upgrade path, and it leaves every pre-existing task
			// NULL.
			//
			// Three steps, in this order, because the order is what makes it
			// safe to run while the engine is serving:
			//
			//  1. Backfill, in batches. One statement over a large inbox holds
			//     row locks for its whole duration; a hundred thousand at a time
			//     keeps each transaction short enough to interleave with work.
			//  2. A DEFAULT, so a writer that omits the column gets 0 rather
			//     than reintroducing the NULL this just removed.
			//  3. SET NOT NULL via a NOT VALID check that is then validated.
			//     SET NOT NULL on its own takes ACCESS EXCLUSIVE and scans the
			//     table under it, blocking every read and write on tasks for the
			//     length of the scan. VALIDATE CONSTRAINT takes only SHARE
			//     UPDATE EXCLUSIVE, and PostgreSQL 12 and later will then accept
			//     SET NOT NULL without rescanning, because the validated check
			//     already proves it.
			Run: func(ctx context.Context, db *gorm.DB) error {
				for {
					res := db.WithContext(ctx).Exec(`
						UPDATE tasks SET priority = 0
						WHERE id IN (SELECT id FROM tasks WHERE priority IS NULL LIMIT 100000)`)
					if res.Error != nil {
						return fmt.Errorf("backfill tasks.priority: %w", res.Error)
					}
					if res.RowsAffected == 0 {
						break
					}
				}

				// The backfill above is portable and is the half that matters
				// for correctness. Everything below is PostgreSQL grammar —
				// ALTER COLUMN, NOT VALID, VALIDATE CONSTRAINT — and SQLite has
				// none of it. PostgreSQL is the only engine this ships on; the
				// completeness suite runs the migration list over in-memory
				// SQLite to prove every model gets a table, and that check must
				// not be broken by a statement only one engine understands.
				if db.Name() != "postgres" {
					return nil
				}

				if err := db.WithContext(ctx).Exec(
					`ALTER TABLE tasks ALTER COLUMN priority SET DEFAULT 0`).Error; err != nil {
					return fmt.Errorf("default tasks.priority: %w", err)
				}

				return setColumnNotNull(ctx, db, "tasks", "priority")
			},
		},
		{
			Version: 22,
			Name:    "every column the reader treats as present is declared present",
			// Migration 21 fixed tasks.priority. Seventy-two columns had the
			// same shape: declared non-null in the storm model — which is what
			// the generated reader is compiled from — and left nullable by
			// AutoMigrate, which does not carry that over. The reader decodes
			// each of them by indexing a fixed number of bytes out of the wire
			// buffer, so one NULL row is `index out of range`, not a zero value,
			// and every read of that table dies on it.
			//
			// They are not reachable through the engine, which writes every
			// field. They are reachable the way tasks.priority was: on an
			// upgrade, when AutoMigrate adds a column to a table that already
			// has rows and leaves every one of them NULL.
			//
			// tests/drift/nullability_test.go is what keeps the list at zero
			// from here; this is the one-off repair for databases that already
			// exist. On a fresh install every column below is already NOT NULL
			// from the model tag, and setColumnNotNull skips it without taking
			// a lock.
			Run: func(ctx context.Context, db *gorm.DB) error {
				if db.Name() != "postgres" {
					return nil
				}

				// Counters and versions, where zero is a meaningful value: no
				// retries yet, no attempts yet, version zero. They get a
				// DEFAULT as well, so a writer that omits the column gets 0
				// rather than putting the NULL straight back.
				counters := []struct{ table, column string }{
					{"connector_manifests", "version"},
					{"decision_definitions", "version"},
					{"environments", "port"},
					{"external_tasks", "retries"},
					{"external_tasks", "retry_timeout"},
					{"idempotency_records", "status_code"},
					{"jobs", "max_retries"},
					{"jobs", "repeats_remaining"},
					{"jobs", "retries"},
					{"process_definition_releases", "version"},
					{"process_definitions", "version"},
					{"service_calls", "attempts"},
					// Storm creates its own tables with the constraint already
					// on, so these are no-ops today. They are listed anyway:
					// the migration is meant to be a complete statement of the
					// invariant, not a list of the columns that happened to be
					// wrong on the day it was written.
					{"participant_sources", "last_run_created"},
					{"participant_sources", "last_run_updated"},
					{"shared_counters", "count"},
				}
				for _, c := range counters {
					if err := backfillAndDefault(ctx, db, c.table, c.column, "0"); err != nil {
						return err
					}
					if err := setColumnNotNull(ctx, db, c.table, c.column); err != nil {
						return err
					}
				}

				// Timestamps get no DEFAULT and no backfill.
				//
				// An invented created_at is a falsified audit trail, and in
				// this system that column is the compliance answer to "when
				// did this happen". Zero of them should be NULL — GORM writes
				// both on every insert — so if any are, something wrote rows
				// around the application and a migration is the wrong place to
				// decide what time they happened. It stops and says so.
				timestamps := []struct{ table, column string }{
					{"compensatable_activities", "completed_at"},
					{"idempotency_records", "created_at"},
					{"jobs", "next_run_at"},
					{"process_definition_releases", "activate_at"},
					{"variable_snapshots", "captured_at"},
					{"webhook_deliveries", "received_at"},
					{"broadcast_events", "created_at"},
					{"shared_counters", "updated_at"},
				}
				for _, table := range baseTimestampTables() {
					timestamps = append(timestamps,
						struct{ table, column string }{table, "created_at"},
						struct{ table, column string }{table, "updated_at"})
				}
				for _, c := range timestamps {
					if err := refuseOnNulls(ctx, db, c.table, c.column); err != nil {
						return err
					}
					if err := setColumnNotNull(ctx, db, c.table, c.column); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			Version: 23,
			Name:    "a service call is one visit to a step, not the step",
			// A service call was identified by (instance, node, iteration), so a
			// process that came back through the same service task — a loop that
			// polls a partner until it is ready — found the first visit's
			// completed call and replayed its response instead of calling. The
			// loop never saw anything change. The visit is the job, one per
			// arrival at the step and reused by its retries, so it joins the
			// identity.
			//
			// Rows written before this carry no job. PostgreSQL treats NULLs as
			// distinct in a unique index, so they never conflict with a row that
			// has one; the repository adopts a legacy row only for the attempt of
			// the visit that wrote it.
			//
			// The new index is built before the old one is dropped, both
			// CONCURRENTLY: service_calls is written on every outbound call, and
			// a moment with neither index would let a retry record a second call.
			//
			// PostgreSQL only, as 21 and 22: anywhere else the baseline has just
			// built the table from the current model, job_id and all.
			Run: func(ctx context.Context, db *gorm.DB) error {
				if db.Name() != "postgres" {
					return nil
				}
				if err := db.WithContext(ctx).Exec(
					`ALTER TABLE service_calls ADD COLUMN IF NOT EXISTS job_id uuid`).Error; err != nil {
					return fmt.Errorf("add service_calls.job_id: %w", err)
				}
				if err := createUniqueIndexConcurrently(ctx, db, "service_calls", "ux_service_calls_visit",
					"instance_id, node_id, iteration_id, job_id"); err != nil {
					return err
				}
				if err := db.WithContext(ctx).Exec(
					`DROP INDEX CONCURRENTLY IF EXISTS ux_service_calls_identity`).Error; err != nil {
					return fmt.Errorf("drop the per-step identity: %w", err)
				}
				return nil
			},
		},
		{
			Version: 27,
			Name:    "an account can be linked to the identity provider that signs it in",
			// Somebody signing in through an OpenID Connect provider is given an
			// account here, and found again by the issuer and subject the
			// provider vouched for — never by an email address, which two
			// providers can each assert. The link is two columns on the account
			// row, so every read of an account says how it signs in without a
			// second query.
			//
			// Unique over the live rows only: an administrator deleting a linked
			// account ends that account, and the person's next sign-in is given a
			// new one rather than refused by a row nobody can see. Built plainly,
			// not CONCURRENTLY: users is small and rarely written, so the lock is
			// held for the milliseconds the build takes, and a plain build cannot
			// leave behind the invalid index a failed concurrent one does.
			//
			// PostgreSQL only, as 21 to 23: anywhere else the baseline has just
			// built the table from the current model, link and index included.
			Run: func(ctx context.Context, db *gorm.DB) error {
				if db.Name() != "postgres" {
					return nil
				}
				for _, stmt := range []string{
					`ALTER TABLE users ADD COLUMN IF NOT EXISTS identity_issuer text`,
					`ALTER TABLE users ADD COLUMN IF NOT EXISTS identity_subject text`,
					`CREATE UNIQUE INDEX IF NOT EXISTS uq_users_identity_issuer_identity_subject
					   ON users (identity_issuer, identity_subject) WHERE deleted_at IS NULL`,
				} {
					if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
						return fmt.Errorf("link accounts to their identity provider: %w", err)
					}
				}
				return nil
			},
		},
	}
}

// baseTimestampTables are the tables whose created_at and updated_at come from
// the embedded models.Base, listed rather than derived: a migration records
// what it did to a database, and one that reads the current model list would
// do something different on every release.
func baseTimestampTables() []string {
	return []string{
		"audit_logs", "compensatable_activities", "connector_instances",
		"connector_manifests", "connectors", "decision_definitions",
		"deployment_resources", "deployments", "environments",
		"event_subscriptions", "external_tasks", "forms", "groups",
		"incidents", "jobs", "notifications", "organizations",
		"process_definition_releases", "process_definitions",
		"process_instances", "projects", "service_calls", "tasks", "users",
		"variable_snapshots", "webhook_deliveries", "webhooks",
		// Storm-owned, and already correct — see the note in migration 22.
		"participant_sources", "platform_roles", "platform_users",
		"workflow_groups", "workflow_users",
	}
}

// columnIsNullable reports whether the column still permits NULL.
//
// Every repair below is a no-op on a database that already has the constraint,
// and checking is what makes the migration cheap on a fresh install and safe to
// re-run after a failure part-way through.
func columnIsNullable(ctx context.Context, db *gorm.DB, table, column string) (bool, error) {
	var nullable string
	err := db.WithContext(ctx).Raw(`
		SELECT is_nullable FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = ? AND column_name = ?`,
		table, column).Scan(&nullable).Error
	if err != nil {
		return false, fmt.Errorf("read %s.%s: %w", table, column, err)
	}
	// An absent column is not an error here: a release that drops one should
	// not make this migration fail forever on the installations that ran it.
	return nullable == "YES", nil
}

// backfillAndDefault fills the NULLs already in a column and stops new ones.
//
// Batched, because one statement over a large table holds row locks for its
// whole duration. On a busy jobs or tasks table that is the difference between
// an upgrade that interleaves with work and one that stops it.
func backfillAndDefault(ctx context.Context, db *gorm.DB, table, column, zero string) error {
	nullable, err := columnIsNullable(ctx, db, table, column)
	if err != nil || !nullable {
		return err
	}
	for {
		res := db.WithContext(ctx).Exec(fmt.Sprintf(
			`UPDATE %[1]s SET %[2]s = %[3]s WHERE ctid IN (
			   SELECT ctid FROM %[1]s WHERE %[2]s IS NULL LIMIT 100000)`,
			table, column, zero))
		if res.Error != nil {
			return fmt.Errorf("backfill %s.%s: %w", table, column, res.Error)
		}
		if res.RowsAffected == 0 {
			break
		}
	}
	if err := db.WithContext(ctx).Exec(fmt.Sprintf(
		`ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s`, table, column, zero)).Error; err != nil {
		return fmt.Errorf("default %s.%s: %w", table, column, err)
	}
	return nil
}

// refuseOnNulls stops the migration rather than inventing a value.
//
// It is reached only for timestamps, where there is no defensible zero. See
// migration 22 for why stopping is the right answer for those and not for a
// counter.
func refuseOnNulls(ctx context.Context, db *gorm.DB, table, column string) error {
	nullable, err := columnIsNullable(ctx, db, table, column)
	if err != nil || !nullable {
		return err
	}
	var nulls int64
	if err := db.WithContext(ctx).Raw(fmt.Sprintf(
		`SELECT count(*) FROM %s WHERE %s IS NULL`, table, column)).Scan(&nulls).Error; err != nil {
		return fmt.Errorf("count nulls in %s.%s: %w", table, column, err)
	}
	if nulls == 0 {
		return nil
	}
	return fmt.Errorf(
		"%s.%s holds %d NULL(s), and every read of %[1]s panics on them.\n\n"+
			"This migration will not guess a timestamp: %[1]s.%[2]s is part of the record of when\n"+
			"things happened, and an invented one is worse than a stopped upgrade. Decide what those\n"+
			"rows should say, set it, and run the upgrade again. If they are junk, delete them",
		table, column, nulls)
}

// setColumnNotNull adds the constraint without locking the table for a scan.
//
// ALTER COLUMN ... SET NOT NULL on its own takes ACCESS EXCLUSIVE and scans the
// whole table under it, which blocks every read and write on it for the length
// of the scan — on process_instances or tasks, during a rolling deploy, while
// the old replica is still serving.
//
// A CHECK added NOT VALID takes the lock only long enough to record itself.
// VALIDATE CONSTRAINT then does the scan under SHARE UPDATE EXCLUSIVE, which
// readers and writers do not contend with. PostgreSQL 12 and later will accept
// SET NOT NULL without rescanning once such a check exists, because the
// validated constraint already proves it, so the exclusive lock is held for a
// catalog update rather than a table scan. The scaffolding is then dropped: two
// constraints saying the same thing is one more thing for every write to check.
func setColumnNotNull(ctx context.Context, db *gorm.DB, table, column string) error {
	nullable, err := columnIsNullable(ctx, db, table, column)
	if err != nil || !nullable {
		return err
	}
	check := fmt.Sprintf("ck_%s_%s_not_null", table, column)

	// Dropped first, so a retry after a failure part-way through does not trip
	// over the constraint its last attempt left behind.
	for _, stmt := range []string{
		fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s`, table, check),
		fmt.Sprintf(`ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s IS NOT NULL) NOT VALID`, table, check, column),
		fmt.Sprintf(`ALTER TABLE %s VALIDATE CONSTRAINT %s`, table, check),
		fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s SET NOT NULL`, table, column),
		fmt.Sprintf(`ALTER TABLE %s DROP CONSTRAINT %s`, table, check),
	} {
		if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	return nil
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
	return buildIndexConcurrently(ctx, db, "INDEX", table, name, columns)
}

// createUniqueIndexConcurrently is createIndexConcurrently for a unique index.
func createUniqueIndexConcurrently(ctx context.Context, db *gorm.DB, table, name, columns string) error {
	return buildIndexConcurrently(ctx, db, "UNIQUE INDEX", table, name, columns)
}

func buildIndexConcurrently(ctx context.Context, db *gorm.DB, kind, table, name, columns string) error {
	if db.Name() != "postgres" {
		if err := db.WithContext(ctx).Exec(fmt.Sprintf(
			"CREATE %s IF NOT EXISTS %s ON %s (%s)", kind, name, table, columns,
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
		"CREATE %s CONCURRENTLY IF NOT EXISTS %s ON %s (%s)", kind, name, table, columns,
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
