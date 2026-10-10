package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// retentionWorld is a server's sweep over a database holding one instance with
// jobs and snapshots of every age and kind the opt-in sweeps choose between.
type retentionWorld struct {
	t      *testing.T
	db     *gorm.DB
	repo   repositories.Repository
	app    *App
	ctx    context.Context
	jobs   map[string]uuid.UUID
	recent uuid.UUID
}

const (
	retainedFor = 30 * 24 * time.Hour
	longAgo     = 60 * 24 * time.Hour
)

func newRetentionWorld(t *testing.T) retentionWorld {
	t.Helper()
	gormDB := testutils.SetupTestDB(t)
	conn := testutils.StormConn(gormDB)
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	w := retentionWorld{
		t: t, db: gormDB, repo: repo,
		app: &App{db: gormDB, storm: conn, repo: repo, sse: sse,
			svc: services.NewServiceFacade(repo, impl.NewEventDispatcher(), sse, "retention-test", nil, nil, nil)},
		ctx:  entities.WithSystemContext(t.Context()),
		jobs: map[string]uuid.UUID{},
	}

	tenant, _, projectID := testutils.ScopedProject(t, repo)
	definitionID := uuid.Must(uuid.NewV7())
	if err := repo.Definition().Create(tenant, models.ProcessDefinitionModel{
		Base: models.Base{ID: models.FromUUID(definitionID)}, ProjectID: models.FromUUID(projectID),
		Key: "claims", Name: "Claims", Version: 1,
	}); err != nil {
		t.Fatalf("seed the definition: %v", err)
	}
	instanceID := uuid.Must(uuid.NewV7())
	if _, err := repo.Process().Create(tenant, models.ProcessInstanceModel{
		Base: models.Base{ID: models.FromUUID(instanceID)}, ProjectID: models.FromUUID(projectID),
		DefinitionID: models.FromUUID(definitionID), Status: models.ProcessActive,
	}); err != nil {
		t.Fatalf("seed the instance: %v", err)
	}

	// Every kind of job, long finished or long due, and one finished lately.
	for name, status := range map[string]models.JobStatus{
		"completed":                  models.JobCompleted,
		"failed, no incident":        models.JobFailed,
		"failed, incident open":      models.JobFailed,
		"failed, incident resolved":  models.JobFailed,
		"pending":                    models.JobPending,
		"running":                    models.JobRunning,
		"completed lately":           models.JobCompleted,
		"completed lately, due long": models.JobCompleted,
	} {
		id, err := repo.Job().Create(w.ctx, models.JobModel{
			InstanceID: models.FromUUID(instanceID), DefinitionID: models.FromUUID(definitionID),
			NodeID: "charge", Type: models.JobServiceTask, Status: status, NextRunAt: time.Now(),
		})
		if err != nil {
			t.Fatalf("seed the %s job: %v", name, err)
		}
		w.jobs[name] = id
		due, changed := time.Now().Add(-longAgo), time.Now().Add(-longAgo)
		switch name {
		case "completed lately":
			due, changed = time.Now().Add(-time.Hour), time.Now().Add(-time.Hour)
		case "completed lately, due long":
			changed = time.Now().Add(-time.Hour)
		}
		w.exec(`UPDATE jobs SET next_run_at = ?, updated_at = ? WHERE id = ?`, due, changed, id)
	}
	for name, status := range map[string]models.IncidentStatus{
		"failed, incident open":     models.IncidentOpen,
		"failed, incident resolved": models.IncidentResolved,
	} {
		if _, err := repo.Incident().Create(tenant, models.IncidentModel{
			JobID: models.FromUUID(w.jobs[name]), InstanceID: models.FromUUID(instanceID),
			DefinitionID: models.FromUUID(definitionID), NodeID: "charge", Error: "declined", Status: status,
		}); err != nil {
			t.Fatalf("seed the incident of the %s job: %v", name, err)
		}
	}

	// More old snapshots than one batch deletes, and one taken lately.
	old, err := repo.VariableSnapshot().Create(tenant, models.VariableSnapshotModel{
		InstanceID: models.FromUUID(instanceID), NodeID: "charge",
		Variables: map[string]any{"amount": 120}, CapturedAt: time.Now().Add(-longAgo),
	})
	if err != nil {
		t.Fatalf("seed an old snapshot: %v", err)
	}
	w.exec(`INSERT INTO variable_snapshots (id, created_at, updated_at, instance_id, node_id, variables, captured_at)
	        SELECT gen_random_uuid(), created_at, updated_at, instance_id, node_id, variables, captured_at
	          FROM variable_snapshots, generate_series(1, ?) WHERE id = ?`, db.PruneBatch, uuid.UUID(old.ID))
	recent, err := repo.VariableSnapshot().Create(tenant, models.VariableSnapshotModel{
		InstanceID: models.FromUUID(instanceID), NodeID: "charge",
		Variables: map[string]any{"amount": 130}, CapturedAt: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("seed a recent snapshot: %v", err)
	}
	w.recent = uuid.UUID(recent.ID)
	return w
}

func (w retentionWorld) exec(stmt string, args ...any) {
	w.t.Helper()
	if err := w.db.WithContext(w.t.Context()).Exec(stmt, args...).Error; err != nil {
		w.t.Fatalf("%s: %v", stmt, err)
	}
}

func (w retentionWorld) count(query string, args ...any) int64 {
	w.t.Helper()
	var n int64
	if err := w.db.WithContext(w.t.Context()).Raw(query, args...).Scan(&n).Error; err != nil {
		w.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (w retentionWorld) jobLeft(name string) bool {
	return w.count(`SELECT count(*) FROM jobs WHERE id = ?`, w.jobs[name]) == 1
}

// Unless an operator says how long to keep them, no snapshot and no job is
// removed, however old: these sweeps are opt-in, and an upgrade must not start
// deleting history nobody asked it to.
func TestNothingIsRemovedByTheOptInSweepsUntilTheyAreConfigured(t *testing.T) {
	w := newRetentionWorld(t)
	snapshots, jobs := w.count(`SELECT count(*) FROM variable_snapshots`), w.count(`SELECT count(*) FROM jobs`)

	w.app.sweepRetention(w.ctx, time.Now(), resolveOptInRetention())

	if left := w.count(`SELECT count(*) FROM variable_snapshots`); left != snapshots {
		t.Errorf("an unconfigured sweep left %d of %d snapshots; want every one", left, snapshots)
	}
	if left := w.count(`SELECT count(*) FROM jobs`); left != jobs {
		t.Errorf("an unconfigured sweep left %d of %d jobs; want every one", left, jobs)
	}
}

// Configured, the sweeps remove the snapshots older than the period — more of
// them than one batch — and the jobs that will never run again, and nothing a
// worker or a person resolving an incident still needs.
func TestTheOptInSweepsRemoveOnlyWhatWillNeverBeNeededAgain(t *testing.T) {
	t.Setenv(envRetainVariableSnapshotsDays, "30")
	t.Setenv(envRetainCompletedJobsDays, "30")
	w := newRetentionWorld(t)
	keep := resolveOptInRetention()
	if keep.variableSnapshots != retainedFor || keep.finishedJobs != retainedFor {
		t.Fatalf("thirty days was read as %v and %v", keep.variableSnapshots, keep.finishedJobs)
	}

	w.app.sweepRetention(w.ctx, time.Now(), keep)

	if left := w.count(`SELECT count(*) FROM variable_snapshots`); left != 1 {
		t.Errorf("%d snapshots are left; want only the one taken within the period", left)
	}
	if w.count(`SELECT count(*) FROM variable_snapshots WHERE id = ?`, w.recent) != 1 {
		t.Error("the snapshot taken within the period was removed")
	}
	for name, wantLeft := range map[string]bool{
		"completed":                  false,
		"failed, no incident":        false,
		"failed, incident resolved":  false,
		"failed, incident open":      true,
		"pending":                    true,
		"running":                    true,
		"completed lately":           true,
		"completed lately, due long": true,
	} {
		if left := w.jobLeft(name); left != wantLeft {
			t.Errorf("the %s job: left = %v, want %v", name, left, wantLeft)
		}
	}
	if w.count(`SELECT count(*) FROM incidents`) != 2 {
		t.Error("a sweep of jobs removed an incident; the account of a failure outlives its job")
	}
}

// A setting that is not a whole number of days is off, not a guess.
func TestARetentionSettingThatIsNotAWholeNumberOfDaysIsOff(t *testing.T) {
	for _, raw := range []string{"", "0", "-3", "30d", "1.5", "99999999"} {
		t.Setenv(envRetainCompletedJobsDays, raw)
		if got := resolveOptInRetention().finishedJobs; got != 0 {
			t.Errorf("%q kept jobs for %v; want the sweep off", raw, got)
		}
	}
	t.Setenv(envRetainCompletedJobsDays, " 7 ")
	if got := resolveOptInRetention().finishedJobs; got != 7*24*time.Hour {
		t.Errorf(`" 7 " kept jobs for %v; want seven days`, got)
	}
}
