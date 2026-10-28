package postgres_test

import (
	"github.com/gsoultan/metis/server/repositories/db"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/interceptors/security"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// What each sweep removes and what it keeps. A sweep picks its rows by what a
// read of the table would select, so these pin the selection: the age, the
// state, and, on a table that soft-deletes, the marked rows as well as the
// live ones — a purge that left the marked rows would never finish them.

func countRows(t *testing.T, db *db.Conn, table string) int64 {
	t.Helper()
	var n int64
	if err := db.Raw("SELECT count(*) FROM " + table).Scan(&n).Error; err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func TestTheIdempotencySweepRemovesOldAnswersAndAbandonedClaims(t *testing.T) {
	db := testutils.SetupTestDB(t)
	conn := db
	now := time.Now().UTC()
	const ttl = time.Hour
	for _, r := range []struct {
		key       string
		completed bool
		age       time.Duration
	}{
		{"old answer", true, 2 * ttl},     // past the TTL: a retry would reclaim it
		{"recent answer", true, ttl / 2},  // a retry can still replay it
		{"running claim", false, 2 * ttl}, // past the TTL, not the day an abandoned one is given
		{"abandoned claim", false, 30 * 24 * time.Hour},
	} {
		if err := db.Exec(`INSERT INTO idempotency_records (record_key, request_hash, completed, status_code, headers, created_at)
			VALUES (?, 'h', ?, 200, '{}', ?)`, r.key, r.completed, now.Add(-r.age)).Error; err != nil {
			t.Fatalf("seed %s: %v", r.key, err)
		}
	}
	removed, err := security.ForgetIdempotencyRecords(t.Context(), conn, ttl, now)
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	if err := db.Raw("SELECT record_key FROM idempotency_records ORDER BY record_key").Scan(&left).Error; err != nil {
		t.Fatal(err)
	}
	if removed != 2 || len(left) != 2 || left[0] != "recent answer" || left[1] != "running claim" {
		t.Errorf("removed %d and left %q; want the old answer and the abandoned claim gone", removed, left)
	}
}

func TestTheSnapshotSweepPurgesOldSnapshotsMarkedOrNot(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ours, _, project := testutils.ScopedProject(t, repo)
	instance := uuid.Must(uuid.NewV7())
	if _, err := repo.Process().Create(ours, models.ProcessInstanceModel{
		Base: models.Base{ID: models.FromUUID(instance)}, ProjectID: models.FromUUID(project),
		DefinitionID: models.FromUUID(seedDefinition(t, db, project, 1)), Status: models.ProcessActive,
	}); err != nil {
		t.Fatalf("seed an instance: %v", err)
	}
	now := time.Now().UTC()
	for _, s := range []struct {
		age     time.Duration
		deleted bool
	}{{48 * time.Hour, false}, {48 * time.Hour, true}, {time.Hour, false}, {time.Hour, true}} {
		var deletedAt any
		if s.deleted {
			deletedAt = now
		}
		if err := db.Exec(`INSERT INTO variable_snapshots (id, created_at, updated_at, instance_id, variables, captured_at, deleted_at)
			VALUES (?, now(), now(), ?, '{}', ?, ?)`, uuid.Must(uuid.NewV7()), instance, now.Add(-s.age), deletedAt).Error; err != nil {
			t.Fatalf("seed a snapshot: %v", err)
		}
	}
	removed, err := repo.VariableSnapshot().ForgetCapturedBefore(entities.WithSystemContext(t.Context()), now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if left := countRows(t, db, "variable_snapshots"); removed != 2 || left != 2 {
		t.Errorf("removed %d and left %d; want both old snapshots gone, the marked one too, and both recent kept", removed, left)
	}
}

func TestTheDeliverySweepPurgesMarkedDeliveriesToo(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ctx := entities.WithSystemContext(t.Context())
	if err := db.Exec(`
		INSERT INTO webhook_deliveries (id, created_at, updated_at, deleted_at, webhook_id, delivery_id, received_at)
		VALUES (gen_random_uuid(), now(), now(), now(), gen_random_uuid(), 'marked', now() - interval '30 days'),
		       (gen_random_uuid(), now(), now(), NULL, gen_random_uuid(), 'live', now() - interval '30 days')`).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	removed, err := repo.Webhook().ForgetDeliveriesBefore(ctx, time.Now().Add(-48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if left := countRows(t, db, "webhook_deliveries"); removed != 2 || left != 0 {
		t.Errorf("removed %d and left %d; want both gone", removed, left)
	}
}

func TestTheJobSweepKeepsWhatIsOwedAndWhatAnOpenIncidentNames(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ours, _, project := testutils.ScopedProject(t, repo)
	definition := seedDefinition(t, db, project, 1)
	instance := uuid.Must(uuid.NewV7())
	if _, err := repo.Process().Create(ours, models.ProcessInstanceModel{
		Base: models.Base{ID: models.FromUUID(instance)}, ProjectID: models.FromUUID(project),
		DefinitionID: models.FromUUID(definition), Status: models.ProcessCompleted,
	}); err != nil {
		t.Fatalf("seed an instance: %v", err)
	}
	old, recent := time.Now().UTC().Add(-30*24*time.Hour), time.Now().UTC()
	job := func(name, status string, at time.Time, marked bool, incidents ...string) {
		t.Helper()
		id := uuid.Must(uuid.NewV7())
		var deletedAt any
		if marked {
			deletedAt = at
		}
		if err := db.Exec(`INSERT INTO jobs (id, created_at, updated_at, deleted_at, instance_id, definition_id, node_id, type, status,
			payload, retries, max_retries, repeats_remaining, next_run_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'timer', ?, '{}', 0, 0, 0, ?)`,
			id, at, at, deletedAt, instance, definition, name, status, at).Error; err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		for _, incident := range incidents {
			var incidentDeleted any
			status := incident
			if incident == "open, marked" {
				status, incidentDeleted = "open", at
			}
			if err := db.Exec(`INSERT INTO incidents (id, created_at, updated_at, deleted_at, instance_id, job_id, node_id, error, status)
				VALUES (?, now(), now(), ?, ?, ?, ?, 'boom', ?)`,
				uuid.Must(uuid.NewV7()), incidentDeleted, instance, id, name, status).Error; err != nil {
				t.Fatalf("seed %s's incident: %v", name, err)
			}
		}
	}
	job("old completed", "completed", old, false)
	job("old completed, marked", "completed", old, true)
	job("recent completed", "completed", recent, false)
	job("old pending", "pending", old, false)
	job("old failed", "failed", old, false)
	job("old failed, resolved", "failed", old, false, "resolved")
	job("old failed, open incident", "failed", old, false, "open", "resolved")
	job("old failed, open incident marked", "failed", old, false, "open, marked")
	job("recent failed", "failed", recent, false)

	removed, err := repo.Job().ForgetFinishedBefore(entities.WithSystemContext(t.Context()), time.Now().Add(-7*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	if err := db.Raw("SELECT node_id FROM jobs ORDER BY node_id").Scan(&left).Error; err != nil {
		t.Fatal(err)
	}
	want := []string{"old failed, open incident", "old pending", "recent completed", "recent failed"}
	if removed != 5 || !slices.Equal(left, want) {
		t.Errorf("removed %d and left %q; want %q", removed, left, want)
	}
}
