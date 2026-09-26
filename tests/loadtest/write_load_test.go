package loadtest

import (
	"context"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// Concurrent writes, and what an orchestrator has to hold while they happen.
//
// The reads in this package are measured against a large database. This
// measures the other direction: many people finishing work at the same moment,
// on a process whose two branches meet at a join. That is where the failures
// that matter live, and a suite that completes one task at a time sees none of
// them:
//
//   - a lost update — two completions each writing the instance without the
//     other's progress — leaves the instance waiting at the join, with no error;
//   - a submission that lands twice advances the process twice;
//   - two transactions taking their locks in different orders deadlock, and a
//     caller is told the server failed for work that was never done.
//
// After both approvals a service task tells a partner, so the job worker runs
// alongside the people as it does in production, and "done once" is checked
// where it costs money: the partner is told once per instance, under one key.
//
//	METIS_TEST_POSTGRES_DSN=... METIS_LOADTEST=1 go test ./tests/loadtest/ -run ConcurrentApprovals -v
//
// Sized with METIS_LOADTEST_WRITE_INSTANCES and METIS_LOADTEST_WRITE_WORKERS.
// METIS_LOADTEST_SEED replays one order of submissions; the seed is logged.

const (
	writeDefinitionKey = "load-parallel-approval"
	financeNode        = "finance"
	legalNode          = "legal"
	joinNode           = "join"

	defaultWriteInstances = 200
	defaultWriteWorkers   = 16

	// Both approvals of one instance are sent at the same instant for one
	// instance in pairOneIn, and one submission in duplicateOneIn is sent twice
	// at once. The rest go in a shuffled order, which puts plenty more side by
	// side anyway.
	pairOneIn      = 4
	duplicateOneIn = 8

	// jobPollInterval is how often an idle job worker looks for work. The
	// default is two seconds; here it bounds how long a finished approval waits
	// for its service task to be picked up, not how fast a busy worker drains.
	jobPollInterval = "100ms"
	// settleTimeout is how long the last instances may take to finish once the
	// last approval is in: the worker's poll, the partner's answer, the advance.
	settleTimeout = time.Minute

	// actionP95Target is .junie/roadmap.md §1's target for a workflow action,
	// the one tests/slo holds a single writer to.
	actionP95Target = 500 * time.Millisecond

	// problemsShown bounds how many problems a failure lists, so two hundred
	// broken instances read as a pattern rather than a wall.
	problemsShown = 10
)

func TestConcurrentApprovalsLoseNothingAndRepeatNothing(t *testing.T) {
	requireOptIn(t)
	instances := intFromEnv("METIS_LOADTEST_WRITE_INSTANCES", defaultWriteInstances)
	// Two at least: the submissions of one move wait for each other, and one
	// worker would be waiting for itself.
	workers := max(intFromEnv("METIS_LOADTEST_WRITE_WORKERS", defaultWriteWorkers), 2)
	seed := seedFromEnv(t)
	w := newWriteLoad(t, workers)

	began := time.Now()
	started, starts := w.client.startInstances(t.Context(), w.projID, instances, workers)
	requireEveryStart(t, starts)
	approvals := w.openApprovals(t, started)
	// #nosec G404 -- a seeded shuffle, so a failing order can be replayed; nothing secret.
	moves := planMoves(rand.New(rand.NewPCG(seed, seed)), approvals)

	completing := time.Now()
	completions := w.client.completeAll(t.Context(), moves, workers, variablesFor(approvals))
	completingTook := time.Since(completing)
	finished := w.awaitEveryInstance(t, len(approvals))
	took := time.Since(began)

	succeeded, refused := checkCompletions(t, approvals, completions)
	if !finished {
		t.Errorf("not every instance finished within %v of the last approval; the checks below say where they stopped", settleTimeout)
	}
	w.checkInstances(t, approvals)
	w.checkLedger(t, approvals)
	checkPartner(t, approvals, w.partner.calls())

	startTimes, completionTimes := timesOf(starts), timesOf(succeeded)
	t.Logf("write load: %d instances, %d writers, seed %d, %d moves (%d submissions)",
		len(approvals), workers, seed, len(moves), len(completions))
	t.Logf("starts       n=%d p50=%v p95=%v p99=%v max=%v", startTimes.n,
		startTimes.p(0.50), startTimes.p(0.95), startTimes.p(0.99), startTimes.p(1.0))
	t.Logf("completions  n=%d p50=%v p95=%v p99=%v max=%v, and %d second submissions refused", completionTimes.n,
		completionTimes.p(0.50), completionTimes.p(0.95), completionTimes.p(0.99), completionTimes.p(1.0), refused)
	t.Logf("throughput   %.1f instances/s end to end (%d in %v); %.0f completions/s while approving (%d in %v)",
		float64(len(approvals))/took.Seconds(), len(approvals), took.Round(time.Millisecond),
		float64(len(succeeded))/completingTook.Seconds(), len(succeeded), completingTook.Round(time.Millisecond))

	for _, measured := range []struct {
		what  string
		times report
	}{{"start", startTimes}, {"completion", completionTimes}} {
		if p95 := measured.times.p(0.95); p95 > actionP95Target {
			t.Errorf("%s p95 is %v under %d concurrent writers, over the %v target for a workflow action",
				measured.what, p95, workers, actionP95Target)
		}
	}
}

// seedFromEnv returns METIS_LOADTEST_SEED, or a fresh seed.
func seedFromEnv(t *testing.T) uint64 {
	t.Helper()
	raw := strings.TrimSpace(os.Getenv("METIS_LOADTEST_SEED"))
	if raw == "" {
		return rand.Uint64() // #nosec G404 -- a shuffle seed, logged; nothing secret.
	}
	seed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		t.Fatalf("METIS_LOADTEST_SEED=%q is not a seed: %v", raw, err)
	}
	return seed
}

// ---------------------------------------------------------------- the harness

// writeLoad is the production handler with a job worker running behind it and a
// partner for the service task to call.
type writeLoad struct {
	*sloHarness
	svc     services.ServiceFacade
	client  *writeClient
	partner *countingPartner
	// ctx is the tenant the checks read as.
	ctx context.Context
}

func newWriteLoad(t *testing.T, workers int) *writeLoad {
	t.Helper()
	// The partner listens on loopback, which the outbound client refuses unless
	// told otherwise; tests/bpmn opts in the same way.
	t.Setenv("METIS_HTTP_ALLOW_PRIVATE_NETWORKS", "true")
	// Read when the job service is built, so it has to be set before the harness.
	t.Setenv("METIS_JOB_POLL_INTERVAL", jobPollInterval)

	partner := newCountingPartner(t)
	base, svc := newSLOHarnessWithService(t)
	w := &writeLoad{
		sloHarness: base,
		svc:        svc,
		client:     newWriteClient(base, workers),
		partner:    partner,
		ctx:        entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: base.orgID.String()}),
	}
	if _, err := svc.CreateDefinition(w.ctx, parallelApprovalDefinition(base.projID, partner.server.URL)); err != nil {
		t.Fatalf("deploy the parallel approval: %v", err)
	}

	svc.StartWorkers(t.Context())
	t.Cleanup(func() {
		// The test's context is already cancelled here, which stops the worker
		// claiming; this waits for what it had claimed, so no job is still
		// writing when the schema is dropped.
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
		defer cancel()
		if err := svc.StopWorkers(stopCtx); err != nil {
			t.Errorf("stop the job worker: %v", err)
		}
	})
	return w
}

// parallelApprovalDefinition is two approvals in parallel, a join, and a partner
// told once both are in.
func parallelApprovalDefinition(projectID uuid.UUID, partnerURL string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     writeDefinitionKey,
		Name:    "Load parallel approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "split", Type: entities.ParallelGateway},
			{ID: financeNode, Name: "Finance approves", Type: entities.UserTask, Assignee: "load"},
			{ID: legalNode, Name: "Legal approves", Type: entities.UserTask, Assignee: "load"},
			{ID: joinNode, Type: entities.ParallelGateway},
			{ID: "notify", Name: "Tell the partner", Type: entities.ServiceTask, Properties: map[string]any{
				"http_url": partnerURL, "http_method": "POST", "output_receipt": "receipt",
			}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "split"},
			{ID: "f2", SourceRef: "split", TargetRef: financeNode},
			{ID: "f3", SourceRef: "split", TargetRef: legalNode},
			{ID: "f4", SourceRef: financeNode, TargetRef: joinNode},
			{ID: "f5", SourceRef: legalNode, TargetRef: joinNode},
			{ID: "f6", SourceRef: joinNode, TargetRef: "notify"},
			{ID: "f7", SourceRef: "notify", TargetRef: "end"},
		},
	}
}

// approval is one instance and the two tasks it waits on.
type approval struct {
	order    string
	instance uuid.UUID
	finance  uuid.UUID
	legal    uuid.UUID
}

// openApprovals finds the two open tasks each instance's split created.
func (w *writeLoad) openApprovals(t *testing.T, started []startedInstance) []approval {
	t.Helper()
	byInstance := w.tasksByInstance(t)
	slices.SortFunc(started, func(a, b startedInstance) int { return strings.Compare(a.order, b.order) })

	approvals := make([]approval, 0, len(started))
	for _, s := range started {
		a := approval{order: s.order, instance: s.id}
		for _, task := range byInstance[s.id] {
			switch task.NodeID {
			case financeNode:
				a.finance = task.ID
			case legalNode:
				a.legal = task.ID
			}
		}
		if len(byInstance[s.id]) != 2 || a.finance == uuid.Nil || a.legal == uuid.Nil {
			t.Fatalf("%s should wait on one finance and one legal approval after the split, and has %d tasks",
				s.order, len(byInstance[s.id]))
		}
		approvals = append(approvals, a)
	}
	return approvals
}

// tasksByInstance is every task in the project, read from the table.
//
// Neither list the API offers is a census at this size. The unpaged one stops
// at the store's thousand-row limit, and the paged one orders by creation time
// alone, which the two tasks a split creates in one transaction share: a page
// boundary between them repeats one and drops the other.
func (w *writeLoad) tasksByInstance(t *testing.T) map[uuid.UUID][]taskRow {
	t.Helper()
	var rows []taskRow
	if err := w.db.Raw(`SELECT id, instance_id, node_id, status FROM tasks
		 WHERE project_id = ? AND deleted_at IS NULL`, w.projID).Scan(&rows).Error; err != nil {
		t.Fatalf("read the tasks: %v", err)
	}
	byInstance := make(map[uuid.UUID][]taskRow, len(rows)/2)
	for _, row := range rows {
		byInstance[row.InstanceID] = append(byInstance[row.InstanceID], row)
	}
	return byInstance
}

// variablesFor gives each approval its own variable, so an update lost between
// the two branches shows as a missing variable on the finished instance.
func variablesFor(approvals []approval) func(uuid.UUID) map[string]any {
	node := make(map[uuid.UUID]string, 2*len(approvals))
	for _, a := range approvals {
		node[a.finance], node[a.legal] = financeNode, legalNode
	}
	return func(taskID uuid.UUID) map[string]any {
		return map[string]any{node[taskID] + "_approved": true}
	}
}

// awaitEveryInstance waits for every instance to finish, and reports whether
// they did.
func (w *writeLoad) awaitEveryInstance(t *testing.T, want int) bool {
	t.Helper()
	deadline := time.Now().Add(settleTimeout)
	for time.Now().Before(deadline) {
		counts, err := w.svc.CountInstancesByStatus(w.ctx, w.projID, repocontracts.InstanceFilter{})
		if err != nil {
			t.Fatalf("count instances: %v", err)
		}
		if counts[models.ProcessCompleted] == int64(want) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
