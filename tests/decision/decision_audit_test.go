package decision_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	handlersimpl "github.com/gsoultan/gobpm/server/domains/handlers/impl"
	observersimpl "github.com/gsoultan/gobpm/server/domains/observers/impl"
	servicecontracts "github.com/gsoultan/gobpm/server/domains/services/contracts"
	serviceimpl "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"github.com/gsoultan/gobpm/tests/testutils"
)

// A decision nobody can trace is an audit finding.
//
// The timeline used to show a variable changing value with nothing to explain
// it: no record of which table decided, which version of that table was in
// force, or which line applied. Six months later — which is when the question
// actually gets asked — the outputs alone answer "what was decided" and not "on
// what grounds", and the table has been edited twice since.
//
// A deployed decision is immutable and versioned, so recording the key, the
// version and the matched lines is enough to replay the reasoning exactly.
func TestADecisionRecordsWhatDecidedAndWhy(t *testing.T) {
	h := newBusinessRuleHarness(t)

	instance := h.runDecision(t, entities.DecisionDefinition{
		Key:       "risk",
		Name:      "Risk band",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "i1", Expression: "amount", Type: "number"}},
		Outputs:   []entities.DecisionOutput{{ID: "o1", Name: "band", Type: "string"}},
		Rules: []entities.DecisionRule{
			{ID: "rule-low", Inputs: []string{"< 100"}, Outputs: []any{"LOW"}},
			{ID: "rule-high", Inputs: []string{">= 100"}, Outputs: []any{"HIGH"}},
		},
	}, map[string]any{"amount": 500.0})

	entry := h.decisionEntry(t, instance.ID)

	if entry.Type != handlersimpl.EventDecisionEvaluated {
		t.Fatalf("type = %q, want %q", entry.Type, handlersimpl.EventDecisionEvaluated)
	}

	// Which table, and which version of it. The version is the whole point: the
	// table it names will have changed by the time anyone reads this.
	if entry.Data["decision_key"] != "risk" {
		t.Errorf("decision_key = %v, want risk", entry.Data["decision_key"])
	}
	if got := entry.Data["decision_version"]; got != float64(1) && got != 1 {
		t.Errorf("decision_version = %v (%T), want 1", got, got)
	}

	// Which line applied — named, not numbered. A position only means something
	// against the version that produced it; an ID survives an edit.
	ids, ok := entry.Data["matched_rule_ids"].([]any)
	if !ok || len(ids) != 1 || ids[0] != "rule-high" {
		t.Errorf("matched_rule_ids = %v, want [rule-high]", entry.Data["matched_rule_ids"])
	}

	// What went in and what came out, so the decision can be replayed.
	inputs, ok := entry.Data["inputs"].(map[string]any)
	if !ok || inputs["amount"] != float64(500) {
		t.Errorf("inputs = %v, want amount 500", entry.Data["inputs"])
	}
	outputs, ok := entry.Data["outputs"].(map[string]any)
	if !ok || outputs["band"] != "HIGH" {
		t.Errorf("outputs = %v, want band HIGH", entry.Data["outputs"])
	}

	// And a line an operator can read without opening the payload.
	if entry.Message != "Risk band (v1) applied rule 2" {
		t.Errorf("message = %q, want it to name the table, the version and the line", entry.Message)
	}
}

// A table that covers nothing is still a decision, and "the policy did not
// apply" is an answer somebody will ask about.
func TestATableThatMatchesNothingIsStillRecorded(t *testing.T) {
	h := newBusinessRuleHarness(t)

	instance := h.runDecision(t, entities.DecisionDefinition{
		Key:       "risk",
		Name:      "Risk band",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "i1", Expression: "amount", Type: "number"}},
		Outputs:   []entities.DecisionOutput{{ID: "o1", Name: "band", Type: "string"}},
		Rules: []entities.DecisionRule{
			{ID: "rule-low", Inputs: []string{"< 100"}, Outputs: []any{"LOW"}},
		},
	}, map[string]any{"amount": 500.0})

	entry := h.decisionEntry(t, instance.ID)
	if entry.Message != "Risk band (v1) matched no rule" {
		t.Errorf("message = %q, want it to say no rule matched", entry.Message)
	}
	if entry.Data["decision_key"] != "risk" {
		t.Errorf("decision_key = %v, want the table that had no opinion named", entry.Data["decision_key"])
	}
}

// The engine must not stall because a note about a decision could not be
// written. The decision has been made; failing the node would retry it, which
// means evaluating the table twice and writing its outputs twice.
func TestAFailedAuditWriteDoesNotStallTheProcess(t *testing.T) {
	h := newBusinessRuleHarnessWithBrokenAudit(t)

	instance := h.runDecision(t, entities.DecisionDefinition{
		Key:       "risk",
		Name:      "Risk band",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "i1", Expression: "amount", Type: "number"}},
		Outputs:   []entities.DecisionOutput{{ID: "o1", Name: "band", Type: "string"}},
		Rules: []entities.DecisionRule{
			{ID: "rule-high", Inputs: []string{">= 100"}, Outputs: []any{"HIGH"}},
		},
	}, map[string]any{"amount": 500.0})

	if instance.Variables["band"] != "HIGH" {
		t.Errorf("band = %v, want HIGH — the decision must stand even when the note about it does not",
			instance.Variables["band"])
	}
	if instance.Status == entities.ProcessFailed {
		t.Error("the process failed because an audit write failed")
	}
}

// auditEntriesOf reads an instance's timeline.
func auditEntriesOf(t *testing.T, h *businessRuleHarness, instanceID uuid.UUID) []models.AuditModel {
	t.Helper()
	entries, err := h.repo.Audit().ListByInstance(t.Context(), instanceID)
	if err != nil {
		t.Fatalf("list audit entries: %v", err)
	}
	return entries
}

// businessRuleHarness runs one business rule task and lets the test read what
// the timeline recorded about it.
type businessRuleHarness struct {
	repo      repositories.Repository
	engine    *serviceimpl.Engine
	decisions servicecontracts.DecisionService
	projectID uuid.UUID
}

func newBusinessRuleHarness(t *testing.T) *businessRuleHarness {
	t.Helper()
	return buildBusinessRuleHarness(t, nil)
}

// newBusinessRuleHarnessWithBrokenAudit gives the handler a writer that always
// fails, which is the only way to check that a decision survives one.
func newBusinessRuleHarnessWithBrokenAudit(t *testing.T) *businessRuleHarness {
	t.Helper()
	return buildBusinessRuleHarness(t, refusingAuditWriter{})
}

type refusingAuditWriter struct{}

func (refusingAuditWriter) RecordEvent(context.Context, entities.AuditEntry) error {
	return errors.New("the audit store is unavailable")
}

func buildBusinessRuleHarness(t *testing.T, audit servicecontracts.AuditWriter) *businessRuleHarness {
	t.Helper()
	db := testutils.SetupTestDB(t)
	ctx := t.Context()
	repo := repositories.NewRepository(db)

	if audit == nil {
		audit = serviceimpl.NewAuditWriter(repo.Audit())
	}

	dispatcher := observersimpl.NewEventDispatcher()
	engine := serviceimpl.NewExecutionEngine(repo, dispatcher)
	connectorSvc := serviceimpl.NewConnectorService(repo)
	taskSvc := serviceimpl.NewTaskService(repo, engine, serviceimpl.NewAuditWriter(repo.Audit()))
	jobSvc := serviceimpl.NewJobService(repo, engine, connectorSvc, serviceimpl.NewNoOpLocker(), handlersimpl.NewErrorBoundaryMatcher())
	externalTaskSvc := serviceimpl.NewExternalTaskService(repo, engine)
	decisionSvc := serviceimpl.NewDecisionService(repo, serviceimpl.NewDecisionTableEvaluator(serviceimpl.NewFEELEvaluator()))

	engine.Apply(
		serviceimpl.WithHandlerFactory(handlersimpl.NewNodeHandlerFactory(
			engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc, repo.Subscription(), audit)),
		serviceimpl.WithJobService(jobSvc),
	)

	org, err := serviceimpl.NewOrganizationService(repo).CreateOrganization(ctx, "Audit Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	project, err := serviceimpl.NewProjectService(repo).CreateProject(ctx, org.ID, "Audit Project", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	return &businessRuleHarness{repo: repo, engine: engine, decisions: decisionSvc, projectID: project.ID}
}

// runDecision deploys the table, runs a process whose only step consults it, and
// returns the finished instance.
func (h *businessRuleHarness) runDecision(t *testing.T, table entities.DecisionDefinition, variables map[string]any) entities.ProcessInstance {
	t.Helper()
	ctx := t.Context()

	table.Project = &entities.Project{ID: h.projectID}
	if _, err := h.decisions.CreateDecision(ctx, table); err != nil {
		t.Fatalf("create decision: %v", err)
	}

	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projectID},
		Key:     "decide-" + table.Key,
		Name:    "Decide",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{
				ID:         "decide",
				Type:       entities.BusinessRuleTask,
				Name:       "Decide",
				Properties: map[string]any{"decision_key": table.Key},
			},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "decide"},
			{ID: "f2", SourceRef: "decide", TargetRef: "end"},
		},
	}
	if _, err := serviceimpl.NewDefinitionService(h.repo).CreateDefinition(ctx, def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.engine.StartProcess(ctx, h.projectID, def.Key, variables)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	return instance
}

// decisionEntry returns the one timeline entry about a decision.
func (h *businessRuleHarness) decisionEntry(t *testing.T, instanceID uuid.UUID) models.AuditModel {
	t.Helper()
	var found []models.AuditModel
	for _, entry := range auditEntriesOf(t, h, instanceID) {
		if entry.Type == handlersimpl.EventDecisionEvaluated {
			found = append(found, entry)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the timeline holds %d decision entries, want exactly one", len(found))
	}
	return found[0]
}

// A decision is a business policy, and a running instance is a commitment made
// under it. Deleting a table an instance is still going to consult turns that
// instance into one that fails at a step which worked yesterday, naming a key
// that no longer exists — and by then nobody remembers what the table said.
func TestADecisionInUseCannotBeDeleted(t *testing.T) {
	h := newBusinessRuleHarness(t)
	ctx := t.Context()

	// A process that stops at a human task after deciding, so its instance is
	// still running and can still be resumed through the decision on a retry.
	table := entities.DecisionDefinition{
		Key:       "risk",
		Name:      "Risk band",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "i1", Expression: "amount", Type: "number"}},
		Outputs:   []entities.DecisionOutput{{ID: "o1", Name: "band", Type: "string"}},
		Rules:     []entities.DecisionRule{{ID: "r1", Inputs: []string{">= 100"}, Outputs: []any{"HIGH"}}},
	}
	instance := h.runDecisionThenWait(t, table, map[string]any{"amount": 500.0})
	if instance.Status != entities.ProcessActive {
		t.Fatalf("instance status = %v, want it still running", instance.Status)
	}

	stored := h.decisionByKey(t, table.Key)
	err := h.decisions.DeleteDecision(ctx, stored.ID)
	if err == nil {
		t.Fatal("the decision was deleted while a running instance could still reach it")
	}
	if !strings.Contains(err.Error(), table.Key) {
		t.Errorf("error = %q, want it to name the decision", err)
	}
	if !strings.Contains(err.Error(), "1 running process instance") {
		t.Errorf("error = %q, want it to say how much is in the way", err)
	}

	// Once nothing is running, it can go.
	h.completeEveryTask(t, instance.ID)
	if err := h.decisions.DeleteDecision(ctx, stored.ID); err != nil {
		t.Errorf("the decision could not be deleted after its instances finished: %v", err)
	}
}

// A decision nothing uses is deleted without ceremony.
func TestAnUnusedDecisionIsDeleted(t *testing.T) {
	h := newBusinessRuleHarness(t)
	ctx := t.Context()

	id, err := h.decisions.CreateDecision(ctx, entities.DecisionDefinition{
		Project:   &entities.Project{ID: h.projectID},
		Key:       "unused",
		Name:      "Unused",
		HitPolicy: entities.HitPolicyFirst,
		Outputs:   []entities.DecisionOutput{{ID: "o1", Name: "x", Type: "string"}},
	})
	if err != nil {
		t.Fatalf("create decision: %v", err)
	}
	if err := h.decisions.DeleteDecision(ctx, id); err != nil {
		t.Errorf("an unused decision could not be deleted: %v", err)
	}
}

// runDecisionThenWait is runDecision with a human task after the decision, so
// the instance is still running when the test looks at it.
func (h *businessRuleHarness) runDecisionThenWait(t *testing.T, table entities.DecisionDefinition, variables map[string]any) entities.ProcessInstance {
	t.Helper()
	ctx := t.Context()

	table.Project = &entities.Project{ID: h.projectID}
	if _, err := h.decisions.CreateDecision(ctx, table); err != nil {
		t.Fatalf("create decision: %v", err)
	}

	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projectID},
		Key:     "decide-and-wait-" + table.Key,
		Name:    "Decide and wait",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "decide", Type: entities.BusinessRuleTask, Name: "Decide",
				Properties: map[string]any{"decision_key": table.Key}},
			{ID: "review", Type: entities.UserTask, Name: "Review"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "decide"},
			{ID: "f2", SourceRef: "decide", TargetRef: "review"},
			{ID: "f3", SourceRef: "review", TargetRef: "end"},
		},
	}
	if _, err := serviceimpl.NewDefinitionService(h.repo).CreateDefinition(ctx, def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.engine.StartProcess(ctx, h.projectID, def.Key, variables)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	return instance
}

func (h *businessRuleHarness) decisionByKey(t *testing.T, key string) entities.DecisionDefinition {
	t.Helper()
	decisions, err := h.decisions.ListDecisions(t.Context(), h.projectID)
	if err != nil {
		t.Fatalf("list decisions: %v", err)
	}
	for _, d := range decisions {
		if d.Key == key {
			return d
		}
	}
	t.Fatalf("no decision with key %q", key)
	return entities.DecisionDefinition{}
}

// completeEveryTask finishes the instance's outstanding work so it stops being
// a reason not to delete anything.
func (h *businessRuleHarness) completeEveryTask(t *testing.T, instanceID uuid.UUID) {
	t.Helper()
	ctx := t.Context()
	tasks, err := h.repo.Task().ListByInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	taskSvc := serviceimpl.NewTaskService(h.repo, h.engine, serviceimpl.NewAuditWriter(h.repo.Audit()))
	for _, m := range tasks {
		if err := taskSvc.CompleteTask(ctx, uuid.UUID(m.ID), "", nil); err != nil {
			t.Fatalf("complete task: %v", err)
		}
	}
}
