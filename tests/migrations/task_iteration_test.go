package migrations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// A task now records which iteration of a multi-instance step it was created
// for. A fresh installation gets the column from the model; an installation
// that predates it has tasks without the column, and that is the only place
// migration 31 does anything.
//
// Nothing is filled in. A task that existed before records no iteration
// afterwards, which is true — nothing recorded it — and is what the engine
// reads as "retire the lowest iteration still waiting".
func TestMigration31GivesEveryTaskAnIterationColumn(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	migrate := func() {
		t.Helper()
		if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
			t.Fatalf("run the migrations: %v", err)
		}
	}
	migrate()
	// A fresh install has the column from the model.
	assertTaskIterationColumn(t, db)

	// tasks as an upgrading installation has it: no column, and a task
	// somebody is already holding.
	if err := db.WithContext(ctx).Exec(`ALTER TABLE tasks DROP COLUMN IF EXISTS iteration_id`).Error; err != nil {
		t.Fatalf("restore the old schema: %v", err)
	}
	if err := db.WithContext(ctx).Exec(
		`DELETE FROM schema_migrations WHERE version = ?`, migrations.TaskIterationMigration).Error; err != nil {
		t.Fatalf("forget that the migration ran: %v", err)
	}
	legacy := uuid.New()
	if err := db.WithContext(ctx).Exec(`
		INSERT INTO tasks (id, created_at, updated_at, project_id, instance_id,
		                   node_id, name, type, status, priority, variables)
		VALUES (?, now(), now(), ?, ?, 'approve', 'Approve', 'userTask', 'unclaimed', 0, '')`,
		legacy, uuid.New(), uuid.New()).Error; err != nil {
		t.Fatalf("write a task as the release before wrote one: %v", err)
	}

	migrate()
	migrate() // and a second run finds nothing left to do

	assertTaskIterationColumn(t, db)
	var row struct {
		Missing bool `gorm:"column:missing"`
	}
	if err := db.WithContext(ctx).Raw(
		`SELECT iteration_id IS NULL AS missing FROM tasks WHERE id = ?`, legacy).Scan(&row).Error; err != nil {
		t.Fatalf("read the task that predates the migration: %v", err)
	}
	if !row.Missing {
		t.Fatal("a task that predates the migration was given an iteration nobody recorded")
	}
}

// Adding the column needs tasks to itself for as long as the catalogue takes,
// and while the ALTER TABLE waits for a reader, PostgreSQL queues every later
// reader and writer of the table behind it. The inbox reads tasks on every
// page a business user opens, so while a canary runs the release's migrations
// beside the stable pods, one long transaction that had read the table would
// stop every inbox for as long as it stayed open. The migration gives up after
// a bounded wait instead, says why, and runs when it is started again.
func TestMigration31GivesUpRatherThanHoldTheInboxBehindALongRead(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	schema := migrations.Schema(models.MigrationModels())
	if _, err := migrations.Run(ctx, db, schema); err != nil {
		t.Fatalf("run the migrations: %v", err)
	}
	if err := db.WithContext(ctx).Exec(`ALTER TABLE tasks DROP COLUMN IF EXISTS iteration_id`).Error; err != nil {
		t.Fatalf("restore the old schema: %v", err)
	}
	if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = ?`,
		migrations.TaskIterationMigration).Error; err != nil {
		t.Fatalf("rewind the migration record: %v", err)
	}

	pool, err := db.DB()
	if err != nil {
		t.Fatalf("open the pool: %v", err)
	}
	reader, err := pool.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin the long read: %v", err)
	}
	defer func() { _ = reader.Rollback() }()
	// Holds ACCESS SHARE on tasks until the transaction ends.
	if _, err := reader.ExecContext(ctx, `SELECT count(*) FROM tasks`); err != nil {
		t.Fatalf("read the tasks: %v", err)
	}

	finished := make(chan error, 1)
	go func() {
		_, err := migrations.Run(context.Background(), db, schema)
		finished <- err
	}()
	const patience = 20 * time.Second
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("the migration altered tasks while a reader held it")
		}
		if !strings.Contains(err.Error(), "tasks was held") {
			t.Errorf("the migration failed without saying what it waited for: %v", err)
		}
	case <-time.After(patience):
		_ = reader.Rollback()
		<-finished
		t.Fatalf("the migration was still waiting for tasks after %s, and every inbox queues behind it", patience)
	}

	// Once the reader is done, starting again finishes the job.
	if err := reader.Rollback(); err != nil {
		t.Fatalf("end the long read: %v", err)
	}
	if _, err := migrations.Run(ctx, db, schema); err != nil {
		t.Fatalf("run the migrations again once the read ended: %v", err)
	}
	assertTaskIterationColumn(t, db)
}

// assertTaskIterationColumn checks the column is text that may be NULL: a task
// of a step that runs once has no iteration, and neither has any task created
// before the migration.
func assertTaskIterationColumn(t *testing.T, db *gorm.DB) {
	t.Helper()
	var column struct {
		DataType   string `gorm:"column:data_type"`
		IsNullable string `gorm:"column:is_nullable"`
	}
	if err := db.WithContext(t.Context()).Raw(`
		SELECT data_type, is_nullable
		  FROM information_schema.columns
		 WHERE table_schema = current_schema() AND table_name = 'tasks' AND column_name = 'iteration_id'`).
		Scan(&column).Error; err != nil {
		t.Fatalf("read tasks.iteration_id: %v", err)
	}
	if column.DataType != "text" {
		t.Fatalf("tasks.iteration_id is %q, want text", column.DataType)
	}
	if column.IsNullable != "YES" {
		t.Error("tasks.iteration_id refuses NULL, which every task of a step that runs once would need")
	}
}
