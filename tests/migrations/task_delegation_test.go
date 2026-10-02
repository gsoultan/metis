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

// rewindTaskDelegation puts tasks back the way a release before migration 32
// had it: no owner, no delegation state, and no record that 32 ran.
func rewindTaskDelegation(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS ix_tasks_owner`,
		`ALTER TABLE tasks DROP COLUMN IF EXISTS owner`,
		`ALTER TABLE tasks DROP COLUMN IF EXISTS delegation_state`,
	} {
		if err := db.WithContext(t.Context()).Exec(stmt).Error; err != nil {
			t.Fatalf("restore the old schema (%s): %v", stmt, err)
		}
	}
	forgetTaskDelegation(t, db)
}

func forgetTaskDelegation(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.WithContext(t.Context()).Exec(
		`DELETE FROM schema_migrations WHERE version = ?`, migrations.TaskDelegationMigration).Error; err != nil {
		t.Fatalf("forget that the migration ran: %v", err)
	}
}

// insertOldTask writes a task the way a release before migration 32 did.
func insertOldTask(t *testing.T, db *gorm.DB, status string, assignee any) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := db.WithContext(t.Context()).Exec(`
		INSERT INTO tasks (id, created_at, updated_at, project_id, instance_id,
		                   node_id, name, type, status, assignee, priority, variables)
		VALUES (?, now(), now(), ?, ?, 'approve', 'Approve', 'userTask', ?, ?, 0, '')`,
		id, uuid.New(), uuid.New(), status, assignee).Error; err != nil {
		t.Fatalf("write a %s task: %v", status, err)
	}
	return id
}

func taskStatusAndAssignee(t *testing.T, db *gorm.DB, id uuid.UUID) (string, string) {
	t.Helper()
	var row struct {
		Status   string
		Assignee string
	}
	if err := db.WithContext(t.Context()).Raw(
		`SELECT status, COALESCE(assignee, '') AS assignee FROM tasks WHERE id = ?`, id).Scan(&row).Error; err != nil {
		t.Fatalf("read the task: %v", err)
	}
	return row.Status, row.Assignee
}

// assertTaskDelegationSchema checks the two columns accept NULL — a task never
// delegated has neither — and that the index a person's "delegated by you"
// list reads through is there and usable.
func assertTaskDelegationSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, column := range []string{"owner", "delegation_state"} {
		var found struct {
			DataType   string `gorm:"column:data_type"`
			IsNullable string `gorm:"column:is_nullable"`
		}
		if err := db.WithContext(t.Context()).Raw(`
			SELECT data_type, is_nullable FROM information_schema.columns
			 WHERE table_schema = current_schema() AND table_name = 'tasks' AND column_name = ?`, column).
			Scan(&found).Error; err != nil {
			t.Fatalf("read tasks.%s: %v", column, err)
		}
		if found.DataType != "character varying" || found.IsNullable != "YES" {
			t.Fatalf("tasks.%s is %q, nullable %q; want a nullable character varying", column, found.DataType, found.IsNullable)
		}
	}
	var valid bool
	if err := db.WithContext(t.Context()).Raw(`
		SELECT i.indisvalid FROM pg_class c JOIN pg_index i ON i.indexrelid = c.oid
		 WHERE c.relname = 'ix_tasks_owner' AND c.relnamespace = current_schema()::regnamespace`).
		Scan(&valid).Error; err != nil {
		t.Fatalf("look for ix_tasks_owner: %v", err)
	}
	if !valid {
		t.Fatal("ix_tasks_owner is missing or not valid; every inbox would scan the whole of tasks for what its reader delegated")
	}
}

// Delegating a task set its status to "delegated" and its assignee to the
// delegate, and recorded nobody to give it back to. Delegation now keeps an
// owner, and a row with none is not one: it is a task its assignee holds. The
// upgrade says so, so that nothing waits for a hand-back nobody can make.
func TestMigration32ReturnsADelegationWithNoOwnerToItsAssignee(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	migrate := func() {
		t.Helper()
		if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
			t.Fatalf("run the migrations: %v", err)
		}
	}
	migrate()
	// A fresh install has the columns and the index from the model.
	assertTaskDelegationSchema(t, db)

	rewindTaskDelegation(t, db)
	delegated := insertOldTask(t, db, "delegated", "citra")
	delegatedToNobody := insertOldTask(t, db, "delegated", nil)
	claimed := insertOldTask(t, db, "claimed", "budi")
	finished := insertOldTask(t, db, "completed", "dita")

	migrate()
	migrate() // and a second run finds nothing left to do

	assertTaskDelegationSchema(t, db)
	for name, want := range map[string]struct {
		id               uuid.UUID
		status, assignee string
	}{
		"a delegation with no owner":      {delegated, "claimed", "citra"},
		"a delegation with nobody at all": {delegatedToNobody, "unclaimed", ""},
		"a claim":                         {claimed, "claimed", "budi"},
		"a completed task":                {finished, "completed", "dita"},
	} {
		if status, assignee := taskStatusAndAssignee(t, db, want.id); status != want.status || assignee != want.assignee {
			t.Errorf("%s is %s, held by %q after the upgrade; want %s, held by %q", name, status, assignee, want.status, want.assignee)
		}
	}

	// A delegation made after the upgrade has an owner, and is left alone by a
	// run that — after a failed deploy, say — happens again.
	pending := insertOldTask(t, db, "delegated", "citra")
	if err := db.WithContext(ctx).Exec(
		`UPDATE tasks SET owner = 'budi', delegation_state = 'pending' WHERE id = ?`, pending).Error; err != nil {
		t.Fatalf("give the delegation an owner: %v", err)
	}
	forgetTaskDelegation(t, db)
	migrate()
	if status, assignee := taskStatusAndAssignee(t, db, pending); status != "delegated" || assignee != "citra" {
		t.Fatalf("a delegation that has an owner is %s, held by %q after the migration ran again; want it untouched", status, assignee)
	}
}

// Adding the columns needs tasks to itself for as long as the catalogue takes,
// and while the ALTER TABLE waits for a reader, PostgreSQL queues every later
// reader and writer of the table behind it — every inbox and every completion,
// while a canary runs the release's migrations beside the stable pods. The
// migration gives up after a bounded wait instead, says why, and runs when it
// is started again.
func TestMigration32GivesUpRatherThanHoldEveryInboxBehindALongRead(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	schema := migrations.Schema(models.MigrationModels())
	if _, err := migrations.Run(ctx, db, schema); err != nil {
		t.Fatalf("run the migrations: %v", err)
	}
	rewindTaskDelegation(t, db)

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

	if err := reader.Rollback(); err != nil {
		t.Fatalf("end the long read: %v", err)
	}
	if _, err := migrations.Run(ctx, db, schema); err != nil {
		t.Fatalf("run the migrations again once the read ended: %v", err)
	}
	assertTaskDelegationSchema(t, db)
}
