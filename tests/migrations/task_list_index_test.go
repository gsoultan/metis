package migrations_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"gorm.io/gorm"
)

// taskPage is the statement the store sends for one page of a project's
// tasks, as it sends it.
const taskPage = `SELECT "id" FROM "tasks"
	WHERE "deleted_at" IS NULL AND ("project_id" = $1)
	ORDER BY "created_at" DESC, "id" DESC LIMIT $2 OFFSET $3`

// A page of a project's tasks is read in order from an index, not by sorting
// every task the project has.
func TestATaskPageIsReadInOrderFromAnIndex(t *testing.T) {
	db := setupMigrated(t)
	assertValidIndex(t, db, "ix_tasks_project_created", "tasks USING btree (project_id, created_at DESC, id DESC)")

	project, other := uuid.New(), uuid.New()
	seedTasks(t, db, project, 2000)
	seedTasks(t, db, other, 2000)
	if err := db.WithContext(t.Context()).Exec(`ANALYZE tasks`).Error; err != nil {
		t.Fatalf("analyze: %v", err)
	}

	plans := explainGeneric(t, db,
		map[string]string{"page": taskPage},
		map[string]string{"page": `'` + project.String() + `', 25, 50`})
	if !strings.Contains(plans["page"], "ix_tasks_project_created") || strings.Contains(plans["page"], "Sort") {
		t.Fatalf("a page of tasks is not read in order from ix_tasks_project_created:\n%s", plans["page"])
	}
}

// Migration 35 is only reachable on an installation that already has tasks.
// This takes the index away and forgets the migration ran, the way an
// upgrading installation arrives, and runs it again.
func TestMigration35IndexesATaskTableThatAlreadyHasRows(t *testing.T) {
	db := setupMigrated(t)
	if err := db.WithContext(t.Context()).Exec(`DROP INDEX IF EXISTS ix_tasks_project_created`).Error; err != nil {
		t.Fatalf("drop the index: %v", err)
	}
	if err := db.WithContext(t.Context()).Exec(
		`DELETE FROM schema_migrations WHERE version = ?`, migrations.TaskListIndexMigration).Error; err != nil {
		t.Fatalf("forget the migration ran: %v", err)
	}
	seedTasks(t, db, uuid.New(), 30)

	migrateAll(t, db)
	migrateAll(t, db) // and a second run finds nothing left to do

	assertValidIndex(t, db, "ix_tasks_project_created", "tasks USING btree (project_id, created_at DESC, id DESC)")
	var kept int64
	if err := db.WithContext(t.Context()).Raw(`SELECT count(*) FROM tasks`).Scan(&kept).Error; err != nil {
		t.Fatalf("count the tasks: %v", err)
	}
	if kept != 30 {
		t.Fatalf("%d of the 30 tasks written before the migration are left", kept)
	}
}

// seedTasks writes count tasks in one project, a second apart, in one
// statement.
func seedTasks(t *testing.T, db *gorm.DB, projectID uuid.UUID, count int) {
	t.Helper()
	if err := db.WithContext(t.Context()).Exec(`
		INSERT INTO tasks (id, created_at, updated_at, project_id, instance_id, node_id, name, type, status, priority)
		SELECT gen_random_uuid(), now() - n * interval '1 second', now(), ?, gen_random_uuid(),
		       'approve', 'Approve ' || n, 'user', 'unclaimed', 0
		  FROM generate_series(1, ?) AS n`,
		projectID, count).Error; err != nil {
		t.Fatalf("seed %d tasks: %v", count, err)
	}
}
