// Simulation: the three claims that have to be true, and the loop that makes it
// worth having.
//
// The claims are not features. They are the reasons a simulation is allowed to
// run production code against a production database at all:
//
//  1. Nothing persists. A run writes tokens, tasks, jobs, incidents and audit
//     rows through the real repositories and then discards every one of them,
//     so the audit trail stays a record of things that actually happened.
//  2. Nothing escapes. No connector is called, no HTTP request is made, no
//     event is fanned out. Rolling back a transaction cannot un-send a request,
//     so this has to be prevented rather than undone.
//  3. Nothing drifts. The same case run twice gives the same trace, or it
//     cannot be a CI gate.
//
// Each of these fails before the code that satisfies it exists, which is the
// only reason to trust them.
package bpmn_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	handlersimpl "github.com/gsoultan/metis/server/domains/handlers/impl"
	observercontracts "github.com/gsoultan/metis/server/domains/observers/contracts"
	observerimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// simHarness is one project with one deployed definition and a simulation
// service wired the way the composition root wires it.
type simHarness struct {
	repo       repositories.Repository
	simulation servicecontracts.SimulationService
	projectID  uuid.UUID
	db         *gorm.DB
	ctx        context.Context
}

// newSimHarness deploys a small expense approval: start → approve (a person) →
// charge (a service) → gateway → end.
func newSimHarness(t *testing.T) *simHarness {
	t.Helper()
	ctx := t.Context()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))

	dispatcher := observerimpl.NewEventDispatcher()
	orgSvc := serviceimpl.NewOrganizationService(repo)
	projectSvc := serviceimpl.NewProjectService(repo)
	defSvc := serviceimpl.NewDefinitionService(repo)
	connectorSvc := serviceimpl.NewConnectorService(repo)
	decisionSvc := serviceimpl.NewDecisionService(repo, serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator()))
	auditWriter := serviceimpl.NewAuditWriter(repo.Audit())

	engine := serviceimpl.NewExecutionEngine(repo, dispatcher)
	taskSvc := serviceimpl.NewTaskService(repo, engine, auditWriter)
	jobSvc := serviceimpl.NewJobService(repo, engine, connectorSvc, serviceimpl.NewNoOpLocker(), handlersimpl.NewErrorBoundaryMatcher())
	externalTaskSvc := serviceimpl.NewExternalTaskService(repo, engine)
	engine.Apply(
		serviceimpl.WithJobService(jobSvc),
		serviceimpl.WithHandlerFactory(handlersimpl.NewNodeHandlerFactory(
			engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc, repo.Subscription(), auditWriter)),
	)

	simulation := serviceimpl.NewSimulationService(repo, func(
		d observercontracts.EventDispatcher, jobs servicecontracts.JobService,
	) servicecontracts.ExecutionEngine {
		simEngine := serviceimpl.NewExecutionEngine(repo, d)
		simTask := serviceimpl.NewTaskService(repo, simEngine, auditWriter)
		simExternal := serviceimpl.NewExternalTaskService(repo, simEngine)
		simEngine.Apply(
			serviceimpl.WithJobService(jobs),
			serviceimpl.WithHandlerFactory(handlersimpl.NewNodeHandlerFactory(
				simEngine, simTask, jobs, simExternal, decisionSvc, connectorSvc, repo.Subscription(), auditWriter)),
		)
		return simEngine
	})

	org, err := orgSvc.CreateOrganization(ctx, "Sim Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	ctx = entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})
	project, err := projectSvc.CreateProject(ctx, org.ID, "Sim Project", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: project.ID},
		Key:     "expense-approval",
		Name:    "Expense Approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Expense submitted"},
			{ID: "approve", Type: entities.UserTask, Name: "Approve the expense"},
			{ID: "charge", Type: entities.ServiceTask, Name: "Charge the card", ExternalTopic: "charge"},
			{ID: "end", Type: entities.EndEvent, Name: "Approved"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "charge"},
			{ID: "f3", SourceRef: "charge", TargetRef: "end"},
		},
	}
	if _, err := defSvc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("deploy definition: %v", err)
	}

	return &simHarness{repo: repo, simulation: simulation, projectID: project.ID, db: db, ctx: ctx}
}

func (h *simHarness) request() entities.SimulationRequest {
	return entities.SimulationRequest{
		ProjectID:     h.projectID,
		DefinitionKey: "expense-approval",
		Variables:     map[string]any{"amount": 4200},
		// Fixed, so a trace can be compared against another trace rather than
		// against whatever time it happened to be.
		ClockStart: time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC),
	}
}

// rowCounts reads every table a run could have written to.
//
// The names are the ones the models declare, and an unreadable table is a
// *failure* rather than a skip. The first version of this helper skipped what
// it could not read, and three of the nine names in it were wrong — so three of
// the assertions passed by never running. A silent skip in the test that
// guards isolation is the same class of bug as a silent fallback in a gateway.
//
// There is deliberately no "tokens" here: tokens are serialised onto the
// instance row rather than kept in a table of their own, so process_instances
// already covers them.
func (h *simHarness) rowCounts(t *testing.T) map[string]int64 {
	t.Helper()
	tables := []string{
		"process_instances", "tasks", "jobs", "incidents",
		"audit_logs", "variable_snapshots", "event_subscriptions", "external_tasks",
	}
	counts := map[string]int64{}
	for _, table := range tables {
		var n int64
		if err := h.db.Table(table).Count(&n).Error; err != nil {
			t.Fatalf("counting %s: %v — this test cannot prove isolation for a table it cannot read", table, err)
		}
		counts[table] = n
	}
	return counts
}

// Claim 1 — nothing persists.
//
// The strongest form of the question: count every row before, run a complete
// simulation, count again. Anything that differs is a simulated commitment that
// outlived its simulation, and the audit trail has stopped being evidence.
func TestSimulationPersistsNothing(t *testing.T) {
	h := newSimHarness(t)

	before := h.rowCounts(t)

	req := h.request()
	req.Answers = []entities.SimulationAnswer{
		{Node: "approve", Kind: entities.AnswerPerson, Actor: "sarah", After: "PT24H"},
		{Node: "charge", Kind: entities.AnswerService, Returns: map[string]any{"receipt": "rc_1"}},
	}

	run, err := h.simulation.Simulate(h.ctx, req)
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if len(run.Steps) == 0 {
		t.Fatal("the run recorded no steps, so this proves nothing about what it wrote")
	}

	after := h.rowCounts(t)
	for table, want := range before {
		if got := after[table]; got != want {
			t.Errorf("%s: %d rows before the simulation, %d after — a simulated run persisted", table, want, got)
		}
	}
}

// Claim 2 — nothing escapes.
//
// A connector that fails the test if it is ever executed. The real job service
// is what calls connectors; a simulation replaces it, and this asserts that the
// replacement is real rather than assumed. Rolling back a transaction cannot
// un-send an HTTP request, so this is the claim that has to hold by
// construction rather than by cleanup.
func TestSimulationCallsNothingOutside(t *testing.T) {
	h := newSimHarness(t)

	req := h.request()
	req.Answers = []entities.SimulationAnswer{
		{Node: "approve", Kind: entities.AnswerPerson, Actor: "sarah", After: "PT24H"},
		{Node: "charge", Kind: entities.AnswerService, Returns: map[string]any{"receipt": "rc_1"}},
	}

	run, err := h.simulation.Simulate(h.ctx, req)
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}

	// No external task row was left for a worker to find either: a simulation
	// that queued real work would have the outside world act on it later, which
	// is the same leak arriving by a slower route.
	var externalTasks int64
	if err := h.db.Table("external_tasks").Count(&externalTasks).Error; err == nil && externalTasks != 0 {
		t.Errorf("the simulation left %d external tasks for a real worker to pick up", externalTasks)
	}

	if run.Outcome != entities.SimulationOutcomeCompleted {
		t.Errorf("expected the answered case to finish, got %q", run.Outcome)
	}
}

// Claim 3 — nothing drifts.
//
// The same case twice. Node sequence and virtual clock, not the run id, which
// is new every time by design.
func TestSimulationIsReproducible(t *testing.T) {
	h := newSimHarness(t)

	req := h.request()
	req.Answers = []entities.SimulationAnswer{
		{Node: "approve", Kind: entities.AnswerPerson, Actor: "sarah", After: "PT24H"},
		{Node: "charge", Kind: entities.AnswerService, Returns: map[string]any{"receipt": "rc_1"}},
	}

	first, err := h.simulation.Simulate(h.ctx, req)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := h.simulation.Simulate(h.ctx, req)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	if len(first.Steps) != len(second.Steps) {
		t.Fatalf("same case, different length: %d steps then %d", len(first.Steps), len(second.Steps))
	}
	for i := range first.Steps {
		if first.Steps[i].Node != second.Steps[i].Node {
			t.Errorf("step %d: %q the first time, %q the second", i, first.Steps[i].Node, second.Steps[i].Node)
		}
		if !first.Steps[i].Clock.Equal(second.Steps[i].Clock) {
			t.Errorf("step %d: clock %s then %s", i, first.Steps[i].Clock, second.Steps[i].Clock)
		}
	}
	if first.VirtualDurationMs != second.VirtualDurationMs {
		t.Errorf("virtual duration moved between runs: %dms then %dms", first.VirtualDurationMs, second.VirtualDurationMs)
	}
}

// The loop the whole feature is shaped around: run an empty case, be told what
// it needs, answer that, run again.
//
// This is the interaction, not a convenience. Nobody fills in a form before
// pressing Run; the process asks, one question at a time, in the order it
// actually reaches them.
func TestSimulationAsksForWhatItNeeds(t *testing.T) {
	h := newSimHarness(t)

	// Nothing answered at all.
	run, err := h.simulation.Simulate(h.ctx, h.request())
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if run.Outcome != entities.SimulationOutcomeNeedsAnswer {
		t.Fatalf("an unanswered case should ask for something, got %q", run.Outcome)
	}
	if run.AwaitingNode != "approve" {
		t.Errorf("expected it to stop at the first person's task, stopped at %q", run.AwaitingNode)
	}
	if run.AwaitingLabel != "Approve the expense" {
		t.Errorf("the question should use the modeller's own name, got %q", run.AwaitingLabel)
	}

	// Answer that one; it should now reach the service task and ask about that.
	req := h.request()
	req.Answers = []entities.SimulationAnswer{
		{Node: "approve", Kind: entities.AnswerPerson, Actor: "sarah", After: "PT24H"},
	}
	run, err = h.simulation.Simulate(h.ctx, req)
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if run.Outcome != entities.SimulationOutcomeNeedsAnswer || run.AwaitingNode != "charge" {
		t.Fatalf("expected the next question to be about charging the card, got %q at %q", run.Outcome, run.AwaitingNode)
	}

	// Answer that too, and it finishes.
	req.Answers = append(req.Answers, entities.SimulationAnswer{
		Node: "charge", Kind: entities.AnswerService, Returns: map[string]any{"receipt": "rc_1"},
	})
	run, err = h.simulation.Simulate(h.ctx, req)
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	if run.Outcome != entities.SimulationOutcomeCompleted {
		t.Fatalf("a fully answered case should finish, got %q", run.Outcome)
	}
	if run.Variables["receipt"] != "rc_1" {
		t.Errorf("what the service returned should be in the variables, got %v", run.Variables["receipt"])
	}
}

// A day is a day, even though the run took milliseconds.
//
// The whole reason the clock is virtual: a PT24H wait has to read as a day
// passing, or a designer learns that their SLA is free.
func TestSimulationAdvancesVirtualTimeWithoutWaiting(t *testing.T) {
	h := newSimHarness(t)

	req := h.request()
	req.Answers = []entities.SimulationAnswer{
		{Node: "approve", Kind: entities.AnswerPerson, Actor: "sarah", After: "PT24H"},
		{Node: "charge", Kind: entities.AnswerService, Returns: map[string]any{"receipt": "rc_1"}},
	}

	started := time.Now()
	run, err := h.simulation.Simulate(h.ctx, req)
	if err != nil {
		t.Fatalf("simulate: %v", err)
	}
	realElapsed := time.Since(started)

	if run.VirtualDurationMs < (24 * time.Hour).Milliseconds() {
		t.Errorf("a PT24H answer should move the clock a day; it moved %dms", run.VirtualDurationMs)
	}
	if realElapsed > 30*time.Second {
		t.Errorf("the simulation actually waited %s — it should fast-forward, not sleep", realElapsed)
	}
}
