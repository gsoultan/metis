package migrations_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// Migration 23 is only reachable from the old schema: a fresh install gets
// job_id and the per-visit index from the model, so on one it finds nothing to
// do. This puts service_calls back the way an upgrading installation has it —
// no job_id, unique per step — with a call already recorded, and runs the
// migration at it.
func TestMigration23MakesAServiceCallPerVisitOnAnExistingTable(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	migrate := func() {
		t.Helper()
		if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
			t.Fatalf("run the migrations: %v", err)
		}
	}
	migrate()

	for _, stmt := range []string{
		`DROP INDEX IF EXISTS ux_service_calls_visit`,
		`ALTER TABLE service_calls DROP COLUMN IF EXISTS job_id`,
		`CREATE UNIQUE INDEX ux_service_calls_identity ON service_calls (instance_id, node_id, iteration_id)`,
		`DELETE FROM schema_migrations WHERE version = 23`,
	} {
		if err := db.WithContext(ctx).Exec(stmt).Error; err != nil {
			t.Fatalf("restore the old schema (%s): %v", stmt, err)
		}
	}
	instance := uuid.New()
	legacy := insertCall(t, db, instance, nil, "legacy-key", time.Now())

	migrate()
	migrate() // and a second run finds nothing left to do

	var identityIndexes int64
	if err := db.WithContext(ctx).Raw(
		`SELECT count(*) FROM pg_indexes WHERE indexname = 'ux_service_calls_identity'`).Scan(&identityIndexes).Error; err != nil {
		t.Fatalf("read the indexes: %v", err)
	}
	if identityIndexes != 0 {
		t.Fatal("the per-step unique index is still there; a loop's second visit cannot record its call")
	}
	var legacyRows, withoutJob int64
	if err := db.WithContext(ctx).Raw(
		`SELECT count(*), count(*) FILTER (WHERE job_id IS NULL) FROM service_calls WHERE id = ?`, legacy).
		Row().Scan(&legacyRows, &withoutJob); err != nil {
		t.Fatalf("read the call recorded before the migration: %v", err)
	}
	if legacyRows != 1 {
		t.Fatal("the call recorded before the migration is gone")
	}
	if withoutJob != 1 {
		t.Fatal("the migration invented a job for an existing call")
	}

	// Two visits to the same step now each have a record.
	first, second := uuid.New(), uuid.New()
	insertCall(t, db, instance, &first, "first-visit", time.Now())
	insertCall(t, db, instance, &second, "second-visit", time.Now())
}

// The one call an upgrade can catch in the middle — recorded by the old
// version, not yet answered or not yet acted on — belongs to the visit that
// made it, and keeps the key the partner has already seen. A record older than
// the visit is a previous pass through a loop, and must not be taken for it.
func TestAnEarlierAttemptIsAdoptedAndAPreviousPassIsNot(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	ctx := entities.WithSystemContext(t.Context())

	written := time.Now().Add(-time.Minute)
	instance := uuid.New()
	insertCall(t, db, instance, nil, "legacy-key", written)

	visit := models.UUID(uuid.New())
	adopted, err := repo.ServiceCall().Begin(ctx, models.ServiceCallModel{
		InstanceID: models.UUID(instance), NodeID: "charge", JobID: &visit, IdempotencyKey: "new-key",
	}, written.Add(-time.Minute))
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if adopted.IdempotencyKey != "legacy-key" || adopted.JobID == nil || *adopted.JobID != visit || adopted.Attempts != 2 {
		t.Fatalf("the earlier attempt was not carried on: key=%q job=%v attempts=%d",
			adopted.IdempotencyKey, adopted.JobID, adopted.Attempts)
	}

	other := uuid.New()
	insertCall(t, db, other, nil, "previous-pass", written)
	laterVisit := models.UUID(uuid.New())
	fresh, err := repo.ServiceCall().Begin(ctx, models.ServiceCallModel{
		InstanceID: models.UUID(other), NodeID: "charge", JobID: &laterVisit, IdempotencyKey: "new-key",
	}, written.Add(time.Minute))
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if fresh.IdempotencyKey != "new-key" || fresh.Attempts != 1 || fresh.Status != models.ServiceCallInFlight {
		t.Fatalf("a previous pass was taken for this visit: key=%q attempts=%d status=%s",
			fresh.IdempotencyKey, fresh.Attempts, fresh.Status)
	}
}

// insertCall records a call as a given version of the engine would have: with
// no job, as before migration 23 — when the column may not exist at all — or
// with one.
func insertCall(t *testing.T, db *gorm.DB, instance uuid.UUID, job *uuid.UUID, key string, at time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	columns := `id, created_at, updated_at, instance_id, project_id, node_id, iteration_id, idempotency_key, status, attempts`
	values := `?, ?, ?, ?, ?, 'charge', '', ?, ?, 1`
	args := []any{id, at, at, instance, uuid.New(), key, models.ServiceCallInFlight}
	if job != nil {
		columns += ", job_id"
		values += ", ?"
		args = append(args, *job)
	}
	if err := db.WithContext(t.Context()).Exec(
		"INSERT INTO service_calls ("+columns+") VALUES ("+values+")", args...).Error; err != nil {
		t.Fatalf("record a call: %v", err)
	}
	return id
}
