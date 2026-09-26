package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/migrations"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// Which version of a decision is live used to be "the highest one". It is a
// timeline entry now, and an installation upgrading has no timeline — so
// migration 26 has to record, for every decision it holds, the version that
// evaluates today. Get that wrong and the upgrade quietly changes a policy:
// a key left with no entry, or with the wrong one, answers differently the
// moment the new build starts.
//
// Only reachable from the old schema: a fresh install has the table and no
// decisions. This builds the database an upgrading installation has — no
// timeline, decisions saved several times, some versions deleted — and runs
// the migrations at it. The rows are written first, through the store, while
// the schema still has the defaults the store writes against: the baseline
// migration re-runs AutoMigrate, which takes them off again.
func TestMigration26MakesTheVersionThatEvaluatesTodayLive(t *testing.T) {
	db := testutils.SetupTestDB(t)
	ctx := entities.WithSystemContext(t.Context())
	repo := repositories.NewRepository(testutils.StormConn(db))
	migrate := func() {
		t.Helper()
		if _, err := migrations.Run(ctx, db, migrations.Schema(models.MigrationModels())); err != nil {
			t.Fatalf("run the migrations: %v", err)
		}
	}
	rewind := func() {
		t.Helper()
		if err := db.WithContext(ctx).Exec(`DELETE FROM schema_migrations WHERE version = ?`,
			migrations.LiveDecisionVersionsMigration).Error; err != nil {
			t.Fatalf("rewind the migration record: %v", err)
		}
	}

	_, _, ours := testutils.ScopedProject(t, repo)
	_, _, theirs := testutils.ScopedProject(t, repo)
	seed := decisionSeeder{t: t, ctx: ctx, repo: repo}
	seed.version(ours, "credit-band", 1, "LOW")
	seed.version(ours, "credit-band", 2, "HIGH")
	seed.version(ours, "risk", 1, "LOW")
	seed.version(ours, "tier", 1, "LOW")
	seed.version(ours, "tier", 2, "MID")
	seed.deleted(ours, "tier", 3, "HIGH")
	seed.deleted(ours, "gone", 1, "LOW")
	seed.version(theirs, "credit-band", 1, "LOW")

	// The schema before 26: no timeline.
	if err := db.WithContext(ctx).Exec(`DROP TABLE IF EXISTS decision_releases`).Error; err != nil {
		t.Fatalf("restore the old schema: %v", err)
	}
	migrate()

	want := map[string]int{
		ours.String() + "/credit-band":   2,
		ours.String() + "/risk":          1,
		ours.String() + "/tier":          2, // the deleted v3 never evaluated
		theirs.String() + "/credit-band": 1, // per project, not per key
	}
	live := liveDecisionVersions(t, db)
	assertLive(t, live, want)

	// Nothing an evaluation returns has changed: the unpinned evaluation of
	// credit-band still reads the version it read before the upgrade.
	decisions := serviceimpl.NewDecisionService(repo, serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator()))
	result, err := decisions.Evaluate(ctx, ours, "credit-band", 0, map[string]any{"score": 20})
	if err != nil {
		t.Fatalf("evaluate credit-band after the upgrade: %v", err)
	}
	if result.Values["band"] != "HIGH" || result.DecisionVersion != 2 {
		t.Errorf("after the upgrade credit-band answers %v from v%d, want HIGH from v2 as before it",
			result.Values["band"], result.DecisionVersion)
	}

	// A second run changes nothing — and in particular does not overrule a
	// version somebody made live after the upgrade.
	rolledBack := time.Now().UTC()
	if err := db.WithContext(ctx).Exec(`
		INSERT INTO decision_releases (id, created_at, updated_at, project_id, decision_key, version, activate_at)
		VALUES (?, ?, ?, ?, 'credit-band', 1, ?)`,
		uuid.Must(uuid.NewV7()).String(), rolledBack, rolledBack, ours.String(), rolledBack).Error; err != nil {
		t.Fatalf("make version 1 live again: %v", err)
	}
	entries := countReleases(t, db)
	rewind()
	migrate()

	want[ours.String()+"/credit-band"] = 1
	assertLive(t, liveDecisionVersions(t, db), want)
	if again := countReleases(t, db); again != entries {
		t.Errorf("a second run wrote %d more timeline entries", again-entries)
	}
}

// decisionSeeder writes decision versions the way a build before the timeline
// did: rows in decision_definitions and nothing else.
type decisionSeeder struct {
	t    *testing.T
	ctx  context.Context
	repo repositories.Repository
}

func (s decisionSeeder) version(project uuid.UUID, key string, version int, band string) uuid.UUID {
	s.t.Helper()
	id := uuid.Must(uuid.NewV7())
	if err := s.repo.Decision().Create(s.ctx, models.DecisionDefinitionModel{
		Base:      models.Base{ID: models.UUID(id)},
		ProjectID: models.UUID(project),
		Key:       key,
		Name:      key,
		Version:   version,
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []models.DecisionInput{{ID: "in", Label: "Score", Expression: "score", Type: "number"}},
		Outputs:   []models.DecisionOutput{{ID: "out", Label: "Band", Name: "band", Type: "string"}},
		Rules:     []models.DecisionRule{{ID: "r1", Inputs: []string{"> 10"}, Outputs: []any{band}}},
	}); err != nil {
		s.t.Fatalf("seed v%d of %s: %v", version, key, err)
	}
	return id
}

func (s decisionSeeder) deleted(project uuid.UUID, key string, version int, band string) {
	s.t.Helper()
	if err := s.repo.Decision().Delete(s.ctx, s.version(project, key, version, band)); err != nil {
		s.t.Fatalf("delete v%d of %s: %v", version, key, err)
	}
}

// liveDecisionVersions reads the timeline the way the engine does: per key, the
// newest entry whose moment has come.
func liveDecisionVersions(t *testing.T, db *gorm.DB) map[string]int {
	t.Helper()
	rows, err := db.WithContext(t.Context()).Raw(`
		SELECT DISTINCT ON (project_id, decision_key) project_id::text, decision_key, version, activate_at
		  FROM decision_releases
		 WHERE deleted_at IS NULL AND activate_at <= now()
		 ORDER BY project_id, decision_key, activate_at DESC`).Rows()
	if err != nil {
		t.Fatalf("read the release timeline: %v", err)
	}
	defer rows.Close()
	live := map[string]int{}
	for rows.Next() {
		var project, key string
		var version int
		var since time.Time
		if err := rows.Scan(&project, &key, &version, &since); err != nil {
			t.Fatalf("read a timeline entry: %v", err)
		}
		live[project+"/"+key] = version
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the release timeline: %v", err)
	}
	return live
}

func assertLive(t *testing.T, got, want map[string]int) {
	t.Helper()
	for key, version := range want {
		if got[key] != version {
			t.Errorf("%s: live version %d, want %d", key, got[key], version)
		}
	}
	for key := range got {
		if _, expected := want[key]; !expected {
			t.Errorf("%s has a live version (%d); every version of it is deleted", key, got[key])
		}
	}
}

func countReleases(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	if err := db.WithContext(t.Context()).Raw(`SELECT count(*) FROM decision_releases`).Scan(&count).Error; err != nil {
		t.Fatalf("count the timeline entries: %v", err)
	}
	return count
}
