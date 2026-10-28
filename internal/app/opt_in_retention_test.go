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
	"github.com/gsoultan/metis/server/repositories/store/incident"
	"github.com/gsoultan/metis/server/repositories/store/job"
	"github.com/gsoultan/metis/server/repositories/store/variablesnapshot"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm/runtime"
)

// retentionWorld is a server's sweep over a database holding one instance with
// jobs and snapshots of every age and kind the opt-in sweeps choose between.
type retentionWorld struct {
	t      *testing.T
	db     *db.Conn
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
	conn := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(conn)
	sse := impl.NewSSEObserver()
	w := retentionWorld{
		t: t, db: conn, repo: repo,
		app: &App{storm: conn, repo: repo, sse: sse,
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
		dated := job.MutateKey(id)
		dated.SetNextRunAt(due)
		dated.SetUpdatedAt(changed)
		w.must("date the "+name+" job", dated.Update(t.Context(), w.ex()))
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
	original, found, err := variablesnapshot.New().IDEq(uuid.UUID(old.ID)).One(t.Context(), w.ex())
	if err != nil || !found {
		t.Fatalf("read the old snapshot back: found %v, %v", found, err)
	}
	copies := make([]variablesnapshot.Row, db.PruneBatch)
	for i := range copies {
		copies[i] = original
		copies[i].ID = uuid.Must(uuid.NewV7())
	}
	_, err = variablesnapshot.InsertAll(t.Context(), w.ex(), copies)
	w.must("copy the old snapshot", err)
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

func (w retentionWorld) ex() runtime.Executor { return testutils.Executor(w.db) }

func (w retentionWorld) must(what string, err error) {
	w.t.Helper()
	if err != nil {
		w.t.Fatalf("%s: %v", what, err)
	}
}

func (w retentionWorld) count(n int64, err error) int64 {
	w.t.Helper()
	w.must("count", err)
	return n
}

func (w retentionWorld) snapshots() int64 {
	return w.count(variablesnapshot.New().Unordered().Count(w.t.Context(), w.ex()))
}

func (w retentionWorld) everyJob() int64 {
	return w.count(job.New().Unordered().Count(w.t.Context(), w.ex()))
}

func (w retentionWorld) jobLeft(name string) bool {
	return w.count(job.New().IDEq(w.jobs[name]).Count(w.t.Context(), w.ex())) == 1
}

// Unless an operator says how long to keep them, no snapshot and no job is
// removed, however old: these sweeps are opt-in, and an upgrade must not start
// deleting history nobody asked it to.
func TestNothingIsRemovedByTheOptInSweepsUntilTheyAreConfigured(t *testing.T) {
	w := newRetentionWorld(t)
	snapshots, jobs := w.snapshots(), w.everyJob()

	w.app.sweepRetention(w.ctx, time.Now(), resolveOptInRetention())

	if left := w.snapshots(); left != snapshots {
		t.Errorf("an unconfigured sweep left %d of %d snapshots; want every one", left, snapshots)
	}
	if left := w.everyJob(); left != jobs {
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

	if left := w.snapshots(); left != 1 {
		t.Errorf("%d snapshots are left; want only the one taken within the period", left)
	}
	if w.count(variablesnapshot.New().IDEq(w.recent).Count(t.Context(), w.ex())) != 1 {
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
	if w.count(incident.New().Unordered().Count(t.Context(), w.ex())) != 2 {
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
