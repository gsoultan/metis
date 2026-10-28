package postgres_test

import (
	"slices"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/repositories/store/idempotencyrecord"
	"github.com/gsoultan/metis/server/repositories/store/incident"
	jobstore "github.com/gsoultan/metis/server/repositories/store/job"
	"github.com/gsoultan/metis/server/repositories/store/variablesnapshot"
	"github.com/gsoultan/metis/server/repositories/store/webhookdelivery"
	"github.com/gsoultan/storm/runtime"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/interceptors/security"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// What each sweep removes and what it keeps. A sweep picks its rows by what a
// read of the table would select, so these pin the selection: the age, the
// state, and, on a table that soft-deletes, the marked rows as well as the
// live ones — a purge that left the marked rows would never finish them.

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
		claim := idempotencyrecord.Create()
		claim.SetRecordKey(r.key)
		claim.SetRequestHash("h")
		claim.SetCompleted(r.completed)
		claim.SetStatusCode(200)
		claim.SetHeaders(runtime.JSON("{}"))
		claim.SetCreatedAt(now.Add(-r.age))
		if _, err := claim.Insert(t.Context(), testutils.Executor(db)); err != nil {
			t.Fatalf("seed %s: %v", r.key, err)
		}
	}
	removed, err := security.ForgetIdempotencyRecords(t.Context(), conn, ttl, now)
	if err != nil {
		t.Fatal(err)
	}
	kept, err := idempotencyrecord.New().Order(idempotencyrecord.RecordKey.Asc()).All(t.Context(), testutils.Executor(db), nil)
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, k := range kept {
		left = append(left, k.RecordKey)
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
		snapshot := variablesnapshot.Row{ID: uuid.Must(uuid.NewV7()), CreatedAt: now, UpdatedAt: now,
			InstanceID: instance, Variables: runtime.JSON("{}"), CapturedAt: now.Add(-s.age)}
		if s.deleted {
			snapshot.DeletedAt = runtime.Null[time.Time]{V: now, Valid: true}
		}
		if _, err := variablesnapshot.InsertAll(t.Context(), testutils.Executor(db), []variablesnapshot.Row{snapshot}); err != nil {
			t.Fatalf("seed a snapshot: %v", err)
		}
	}
	removed, err := repo.VariableSnapshot().ForgetCapturedBefore(entities.WithSystemContext(t.Context()), now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if left := testutils.RowsIn(t, db, "variable_snapshots"); removed != 2 || left != 2 {
		t.Errorf("removed %d and left %d; want both old snapshots gone, the marked one too, and both recent kept", removed, left)
	}
}

func TestTheDeliverySweepPurgesMarkedDeliveriesToo(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ctx := entities.WithSystemContext(t.Context())
	hook := testutils.SeedWebhook(t, db, testutils.SeedProject(t, db))
	now := time.Now()
	month := now.Add(-30 * 24 * time.Hour)
	if _, err := webhookdelivery.InsertAll(ctx, testutils.Executor(db), []webhookdelivery.Row{
		{ID: uuid.Must(uuid.NewV7()), CreatedAt: now, UpdatedAt: now, DeletedAt: runtime.Null[time.Time]{V: now, Valid: true},
			WebhookID: hook, DeliveryID: "marked", ReceivedAt: month},
		{ID: uuid.Must(uuid.NewV7()), CreatedAt: now, UpdatedAt: now, WebhookID: hook, DeliveryID: "live", ReceivedAt: month},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	removed, err := repo.Webhook().ForgetDeliveriesBefore(ctx, time.Now().Add(-48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if left := testutils.RowsIn(t, db, "webhook_deliveries"); removed != 2 || left != 0 {
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
		ex := testutils.Executor(db)
		row := jobstore.Row{ID: id, CreatedAt: at, UpdatedAt: at, InstanceID: instance, DefinitionID: definition,
			NodeID: name, Type: "timer", Status: status, Payload: runtime.JSON("{}"), NextRunAt: at}
		if marked {
			row.DeletedAt = runtime.Null[time.Time]{V: at, Valid: true}
		}
		if _, err := jobstore.InsertAll(t.Context(), ex, []jobstore.Row{row}); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		for _, inc := range incidents {
			stamp := time.Now()
			row := incident.Row{ID: uuid.Must(uuid.NewV7()), CreatedAt: stamp, UpdatedAt: stamp, InstanceID: instance,
				JobID: runtime.Null[[16]byte]{V: id, Valid: true}, NodeID: name, Error: "boom", Status: inc}
			if inc == "open, marked" {
				row.Status, row.DeletedAt = "open", runtime.Null[time.Time]{V: at, Valid: true}
			}
			if _, err := incident.InsertAll(t.Context(), ex, []incident.Row{row}); err != nil {
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
	kept, err := jobstore.New().Order(jobstore.NodeID.Asc()).All(t.Context(), testutils.Executor(db), nil)
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	for _, k := range kept {
		left = append(left, k.NodeID)
	}
	want := []string{"old failed, open incident", "old pending", "recent completed", "recent failed"}
	if removed != 5 || !slices.Equal(left, want) || testutils.RowsIn(t, db, "jobs") != int64(len(want)) {
		t.Errorf("removed %d and left %q; want %q", removed, left, want)
	}
}
