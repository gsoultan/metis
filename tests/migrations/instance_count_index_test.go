package migrations_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"gorm.io/gorm"
)

const instanceCountIndex = "process_instances USING btree (project_id, definition_id, status) WHERE (deleted_at IS NULL)"

// countByStatus and countByDefinition are the statements the store sends for
// the instance list's status counts and the version history's, as it sends
// them.
const (
	countByStatus = `SELECT "status" AS "status", count(*) AS "total" FROM "process_instances"
		WHERE "deleted_at" IS NULL AND ("project_id" = $1)
		GROUP BY "status" ORDER BY "status" LIMIT $2`
	countByDefinition = `SELECT "definition_id" AS "definition_id", "status" AS "status", count(*) AS "total" FROM "process_instances"
		WHERE "deleted_at" IS NULL AND ("definition_id" = ANY($1) AND "project_id" = $2)
		GROUP BY "definition_id", "status" ORDER BY "definition_id", "status" LIMIT $3`
)

// The instance counts are answered from the index alone, without a visit to
// any instance, and still are once the statement is planned for any
// parameters at all.
func TestInstanceCountsAreAnsweredFromTheIndexAlone(t *testing.T) {
	db := setupMigrated(t)
	assertValidIndex(t, db, "ix_process_instances_live_counts", instanceCountIndex)

	project, definition := seedInstanceProject(t, db)
	other, otherDefinition := seedInstanceProject(t, db)
	seedInstances(t, db, project, definition, 2000)
	seedInstances(t, db, other, otherDefinition, 2000)
	if err := db.WithContext(t.Context()).Exec(`VACUUM ANALYZE process_instances`).Error; err != nil {
		t.Fatalf("vacuum: %v", err)
	}

	plans := explainGeneric(t, db, map[string]string{
		"by_status":     countByStatus,
		"by_definition": countByDefinition,
	}, map[string]string{
		"by_status":     `'` + project.String() + `', 1000`,
		"by_definition": `'{` + definition.String() + `}'::uuid[], '` + project.String() + `', 1000`,
	})
	for name, plan := range plans {
		if !strings.Contains(plan, "Index Only Scan using ix_process_instances_live_counts") {
			t.Errorf("%s is not answered from ix_process_instances_live_counts alone:\n%s", name, plan)
		}
	}
}

// Migration 47 is only reachable on an installation that already has
// instances. This takes the index away and forgets the migration ran, the way
// an upgrading installation arrives, and runs it again.
func TestMigration47IndexesAnInstanceTableThatAlreadyHasRows(t *testing.T) {
	db := setupMigrated(t)
	if err := db.WithContext(t.Context()).Exec(`DROP INDEX IF EXISTS ix_process_instances_live_counts`).Error; err != nil {
		t.Fatalf("drop the index: %v", err)
	}
	if err := db.WithContext(t.Context()).Exec(
		`DELETE FROM schema_migrations WHERE version = ?`, migrations.InstanceCountIndexMigration).Error; err != nil {
		t.Fatalf("forget the migration ran: %v", err)
	}
	project, definition := seedInstanceProject(t, db)
	seedInstances(t, db, project, definition, 30)

	migrateAll(t, db)
	migrateAll(t, db) // and a second run finds nothing left to do

	assertValidIndex(t, db, "ix_process_instances_live_counts", instanceCountIndex)
	var kept int64
	if err := db.WithContext(t.Context()).Raw(`SELECT count(*) FROM process_instances`).Scan(&kept).Error; err != nil {
		t.Fatalf("count the instances: %v", err)
	}
	if kept != 30 {
		t.Fatalf("%d of the 30 instances written before the migration are left", kept)
	}
}

// seedInstanceProject makes an organization, a project in it and one version
// of a definition in that.
func seedInstanceProject(t *testing.T, db *gorm.DB) (project, definition uuid.UUID) {
	t.Helper()
	organization := uuid.New()
	project, definition = uuid.New(), uuid.New()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO organizations (id, created_at, updated_at, name) VALUES (?, now(), now(), ?)`,
			[]any{organization, "org-" + organization.String()}},
		{`INSERT INTO projects (id, created_at, updated_at, organization_id, name) VALUES (?, now(), now(), ?, ?)`,
			[]any{project, organization, "project-" + project.String()}},
		{`INSERT INTO process_definitions (id, created_at, updated_at, project_id, key, name, version, nodes, flows)
		  VALUES (?, now(), now(), ?, 'order', 'Order', 1, '[]', '[]')`,
			[]any{definition, project}},
	} {
		if err := db.WithContext(t.Context()).Exec(stmt.sql, stmt.args...).Error; err != nil {
			t.Fatalf("seed a project: %v", err)
		}
	}
	return project, definition
}

// seedInstances writes count instances of one definition, in every state and
// a few of them deleted, in one statement.
func seedInstances(t *testing.T, db *gorm.DB, project, definition uuid.UUID, count int) {
	t.Helper()
	if err := db.WithContext(t.Context()).Exec(`
		INSERT INTO process_instances (id, created_at, updated_at, project_id, definition_id, status,
		                               variables, tokens, completed_nodes, compensated_nodes, multi_instance, joins, deleted_at)
		SELECT gen_random_uuid(), now() - n * interval '1 second', now(), ?, ?,
		       (ARRAY['active', 'completed', 'failed', 'suspended'])[1 + n % 4],
		       '', '[]', '[]', '[]', '{}', '{}', CASE WHEN n % 50 = 0 THEN now() END
		  FROM generate_series(1, ?) AS n`,
		project, definition, count).Error; err != nil {
		t.Fatalf("seed %d instances: %v", count, err)
	}
}
