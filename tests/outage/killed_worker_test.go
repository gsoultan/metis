package outage

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// A worker's database connection killed in the middle of a job.
//
// A service task's job makes the partner's call outside any transaction, so
// that one slow partner cannot hold a row lock on the instance, and then does
// two writes: it records the partner's answer, and it advances the process
// with the job's completion in the same transaction. A connection can die at
// either — a failover, a proxy restart, an administrator's pg_terminate_backend
// — and the job must then not be lost, must not call the partner twice under
// different keys, and must still finish the instance.
//
// The worker's backend is found by what it is doing: it is made to wait on a
// row the test holds, so pg_blocking_pids names it, whatever else is running
// on the server. It is killed there and the row released.

// faultTimeout bounds each wait for the worker to reach, or leave, a step.
const faultTimeout = 15 * time.Second

// heldRow is a row the test holds while the worker needs it.
type heldRow struct {
	// lock takes the row, in the test's transaction.
	lock string
	// statement is what the worker is running when it waits on the row.
	statement string
}

var (
	// recordingTheAnswer is where callOnce records the partner's response.
	recordingTheAnswer = heldRow{
		lock:      `SELECT id FROM service_calls WHERE instance_id = ? FOR UPDATE`,
		statement: `UPDATE "service_calls"`,
	}
	// advancingTheProcess is the transaction that moves the token on and
	// completes the job.
	advancingTheProcess = heldRow{
		lock:      `SELECT id FROM process_instances WHERE id = ? FOR UPDATE`,
		statement: `FROM "process_instances"`,
	}
)

func TestAWorkerWhoseConnectionIsKilledMidJobFinishesTheJobOnce(t *testing.T) {
	testCases := []struct {
		name string
		// killedAt are the steps where the worker's connection is killed, in
		// the order the worker reaches them.
		killedAt []heldRow
		// partnerCalls is how many calls the partner sees in all: a second
		// one is the window no client can close — the answer arrived and could
		// not be recorded — and it has to carry the same key as the first.
		partnerCalls int
	}{
		{
			name:         "while advancing the process, after the answer was recorded",
			killedAt:     []heldRow{advancingTheProcess},
			partnerCalls: 1,
		},
		{
			name:         "while recording the answer, and again while advancing",
			killedAt:     []heldRow{recordingTheAnswer, advancingTheProcess},
			partnerCalls: 2,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			d := newKillDrill(t)

			d.worker.Go(func() { d.roundErr = d.jobs.ProcessPendingJobs(d.workerCtx) })
			d.partner.awaitFirstCall(t)
			d.requireNothingHeldOpen(t)

			holder := d.hold(t, tc.killedAt)
			d.partner.answer()
			for _, step := range tc.killedAt {
				d.killTheWorkerAt(t, holder, step)
			}
			holder.release(t)
			d.awaitRound(t)

			d.requireFailureRecorded(t)
			d.retryWhenDue(t)
			d.requireFinishedOnce(t, tc.partnerCalls)
		})
	}
}

// ---------------------------------------------------------------- the drill

type killDrill struct {
	db       *gorm.DB
	repo     repositories.Repository
	engine   *serviceimpl.Engine
	jobs     servicecontracts.JobService
	partner  *blockingPartner
	ctx      context.Context
	instance uuid.UUID
	// appName is the application_name every connection this test opens
	// carries, so pg_stat_activity can tell the worker's from the rest of the
	// server's.
	appName string

	workerCtx context.Context
	worker    sync.WaitGroup
	roundErr  error
}

func newKillDrill(t *testing.T) *killDrill {
	t.Helper()
	appName := "metis-outage-" + uuid.NewString()[:8]
	// Read by pgx when a connection is configured, so every pool the harness
	// opens below carries it.
	t.Setenv("PGAPPNAME", appName)
	// The partner listens on loopback, which the outbound client refuses
	// unless told otherwise.
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")

	db := testutils.SetupPostgresDB(t, 4)
	repo, engine, jobs, projectID, ctx := newEngine(t, db)
	d := &killDrill{
		db: db, repo: repo, engine: engine, jobs: jobs, ctx: ctx, appName: appName,
		partner: newBlockingPartner(t),
		// The job worker spans every tenant, as StartWorkers runs it.
		workerCtx: entities.WithSystemContext(ctx),
	}
	t.Cleanup(d.worker.Wait)

	if _, err := serviceimpl.NewDefinitionService(repo).CreateDefinition(ctx, chargeDefinition(projectID, d.partner.server.URL)); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	instance, err := engine.StartProcess(ctx, projectID, "charge-once", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	d.instance = instance
	return d
}

// chargeDefinition is start → charge a card → end: the step where running twice
// costs somebody money.
func chargeDefinition(projectID uuid.UUID, partnerURL string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "charge-once",
		Name:    "Charge once",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "charge", Type: entities.ServiceTask, Properties: map[string]any{
				"http_url": partnerURL, "http_method": "POST", "output_charge_id": "chargeId",
			}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "charge"},
			{ID: "f2", SourceRef: "charge", TargetRef: "end"},
		},
	}
}

// requireNothingHeldOpen checks that, with the partner on the line, the worker
// holds no transaction open. That is why a connection lost during the call
// costs nothing — and why the faults below are aimed at the writes after it.
func (d *killDrill) requireNothingHeldOpen(t *testing.T) {
	t.Helper()
	var named, busy int64
	if err := d.db.Raw(`SELECT count(*), count(*) FILTER (WHERE state IS DISTINCT FROM 'idle')
		  FROM pg_stat_activity WHERE application_name = ? AND pid <> pg_backend_pid()`, d.appName).
		Row().Scan(&named, &busy); err != nil {
		t.Fatalf("read pg_stat_activity: %v", err)
	}
	if named == 0 {
		t.Fatal("no connection carries this test's application_name, so nothing below can tell the worker's from any other")
	}
	if busy > 0 {
		t.Fatalf("%d of the engine's connections are in a transaction or a statement while the partner's call is in flight; "+
			"a network call inside a transaction holds its locks for as long as the partner takes", busy)
	}
}

// awaitRound waits for the worker's round of jobs to come back.
func (d *killDrill) awaitRound(t *testing.T) {
	t.Helper()
	finished := make(chan struct{})
	go func() {
		d.worker.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(faultTimeout):
		t.Fatal("the worker never came back from the job whose connection was killed")
	}
	if d.roundErr != nil {
		t.Fatalf("the round of jobs failed: %v", d.roundErr)
	}
}

// requireFailureRecorded checks that the job lost to the killed connection is
// neither gone nor running: it is waiting to be tried again, with an attempt
// counted and the error on it, and the instance has not moved.
func (d *killDrill) requireFailureRecorded(t *testing.T) {
	t.Helper()
	job := d.onlyJob(t)
	if job.Status != models.JobPending || job.Retries != 1 {
		t.Fatalf("after the connection was killed the job is %s with %d retries; want pending with one attempt counted",
			job.Status, job.Retries)
	}
	if job.LastError == "" {
		t.Fatal("the job failed and recorded no error, so nobody looking at it can tell why it is waiting")
	}
	t.Logf("recorded on the job: %s", job.LastError)

	instance, err := d.engine.GetInstance(d.ctx, d.instance)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	if instance.Status != entities.ProcessActive || len(instance.GetTokensByNode(&entities.Node{ID: "charge"})) != 1 {
		t.Fatalf("after the failed attempt the instance is %s with tokens %+v; want it still waiting on the charge",
			instance.Status, instance.Tokens)
	}
}

// retryWhenDue brings the retry forward, as the backoff would, and runs it.
func (d *killDrill) retryWhenDue(t *testing.T) {
	t.Helper()
	job := d.onlyJob(t)
	job.NextRunAt = time.Now().Add(-time.Minute)
	if err := d.repo.Job().Update(d.ctx, job); err != nil {
		t.Fatalf("bring the retry forward: %v", err)
	}
	if err := d.jobs.ProcessPendingJobs(d.workerCtx); err != nil {
		t.Fatalf("run the retry: %v", err)
	}
}

// requireFinishedOnce checks the end state: the instance completed on the
// partner's answer, the job is done, and the partner was called as often as
// the fault allows and never under a second key.
func (d *killDrill) requireFinishedOnce(t *testing.T, partnerCalls int) {
	t.Helper()
	instance, err := d.engine.GetInstance(d.ctx, d.instance)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	if instance.Status != entities.ProcessCompleted || instance.Variables["chargeId"] != "ch_1" {
		t.Fatalf("the instance is %s with chargeId %v; want it completed on the partner's answer",
			instance.Status, instance.Variables["chargeId"])
	}
	if job := d.onlyJob(t); job.Status != models.JobCompleted {
		t.Fatalf("the job is %s after its retry; want completed", job.Status)
	}

	keys := d.partner.keysSeen()
	if len(keys) != partnerCalls {
		t.Fatalf("the partner was called %d times; want %d", len(keys), partnerCalls)
	}
	if distinct := slices.Compact(slices.Sorted(slices.Values(keys))); len(distinct) != 1 || distinct[0] == "" {
		t.Fatalf("the partner was called under keys %q; every call for one charge has to carry the same key, "+
			"or the partner cannot tell a repeat from a second charge", keys)
	}

	call, err := d.repo.ServiceCall().Get(d.ctx, d.instance, "charge", "")
	if err != nil {
		t.Fatalf("read the service call record: %v", err)
	}
	if call.Status != models.ServiceCallCompleted || call.Attempts != partnerCalls {
		t.Fatalf("the service call is recorded %s after %d attempts; want completed after %d",
			call.Status, call.Attempts, partnerCalls)
	}
}

func (d *killDrill) onlyJob(t *testing.T) models.JobModel {
	t.Helper()
	jobs, err := d.repo.Job().ListByInstance(d.ctx, d.instance)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected the charge's one job, found %d", len(jobs))
	}
	return jobs[0]
}
