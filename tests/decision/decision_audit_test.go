package decision_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/adapters"
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

// Pattern 3 from the plan: the decision decides who does the work.
//
// An approval matrix is business policy — it changes when the org changes, which
// is far more often than the process does. Written into the diagram, moving the
// threshold from 10k to 25k takes a modeller and a redeploy.
func TestADecisionDecidesWhoApproves(t *testing.T) {
	h := newBusinessRuleHarness(t)
	ctx := t.Context()

	matrix := entities.DecisionDefinition{
		Project:   &entities.Project{ID: h.projectID},
		Key:       "approval-matrix",
		Name:      "Who approves",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "i1", Expression: "amount", Type: "number"}},
		Outputs: []entities.DecisionOutput{
			{ID: "o1", Name: "assignee", Type: "string"},
			{ID: "o2", Name: "priority", Type: "number"},
		},
		Rules: []entities.DecisionRule{
			{ID: "big", Inputs: []string{">= 10000"}, Outputs: []any{"cfo", 9}},
			{ID: "small", Inputs: []string{"-"}, Outputs: []any{"team.lead", 1}},
		},
	}
	if _, err := h.decisions.CreateDecision(ctx, matrix); err != nil {
		t.Fatalf("create decision: %v", err)
	}

	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projectID},
		Key:     "expense",
		Name:    "Expense approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{
				ID:   "approve",
				Type: entities.UserTask,
				Name: "Approve the expense",
				// The diagram says somebody approves; the table says who.
				Assignee:   "team.lead",
				Properties: map[string]any{handlersimpl.AssignmentDecisionKey: "approval-matrix"},
			},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "end"},
		},
	}
	if _, err := serviceimpl.NewDefinitionService(h.repo).CreateDefinition(ctx, def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	// A large expense goes to the CFO, not to the diagram's default.
	big, err := h.engine.StartProcess(ctx, h.projectID, "expense", map[string]any{"amount": 25000.0})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	task := h.taskOn(t, big)
	if task.Assignee == nil || task.Assignee.Username != "cfo" {
		t.Errorf("assignee = %+v, want cfo — the table decided, not the diagram", task.Assignee)
	}
	if task.Priority != 9 {
		t.Errorf("priority = %d, want 9", task.Priority)
	}

	// A small one goes where the table says, which happens to match the default.
	small, err := h.engine.StartProcess(ctx, h.projectID, "expense", map[string]any{"amount": 100.0})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	if got := h.taskOn(t, small); got.Assignee == nil || got.Assignee.Username != "team.lead" {
		t.Errorf("assignee = %+v, want team.lead", got.Assignee)
	}

	// And the choice is on the timeline, because "why did this land on the CFO's
	// desk?" is asked about approvals more than about anything else.
	entry := h.decisionEntry(t, big)
	if entry.Data["decided"] != "assignment" {
		t.Errorf("the timeline entry does not say it decided the assignment: %v", entry.Data)
	}
	if !strings.Contains(entry.Message, "cfo") {
		t.Errorf("message = %q, want it to name who it chose", entry.Message)
	}
}

func (h *businessRuleHarness) taskOn(t *testing.T, instanceID uuid.UUID) entities.Task {
	t.Helper()
	tasks, err := h.repo.Task().ListByInstance(t.Context(), instanceID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("the instance has %d tasks, want one", len(tasks))
	}
	return adapters.TaskEntityAdapter{Model: tasks[0]}.ToEntity()
}

// A decision table is a policy several processes can share, and the person
// about to edit one is usually the person least able to see who else depends on
// it. Changing a threshold with instances part-way through is a different act
// from changing one nothing uses.
func TestTheImpactViewSaysWhatDependsOnADecision(t *testing.T) {
	h := newBusinessRuleHarness(t)
	ctx := t.Context()

	table := entities.DecisionDefinition{
		Key:       "risk",
		Name:      "Risk band",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "i1", Expression: "amount", Type: "number"}},
		Outputs:   []entities.DecisionOutput{{ID: "o1", Name: "band", Type: "string"}},
		Rules:     []entities.DecisionRule{{ID: "r1", Inputs: []string{"-"}, Outputs: []any{"HIGH"}}},
	}
	instance := h.runDecisionThenWait(t, table, map[string]any{"amount": 500.0})
	stored := h.decisionByKey(t, table.Key)

	impact, err := h.decisions.DecisionImpact(ctx, stored.ID)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}

	if len(impact.Processes) != 1 {
		t.Fatalf("the impact names %d processes, want one", len(impact.Processes))
	}
	process := impact.Processes[0]
	if process.DefinitionKey != "decide-and-wait-risk" {
		t.Errorf("definition = %q, want the process that consults it", process.DefinitionKey)
	}
	// Named steps, not a count: "Decide" tells somebody where the policy is used.
	if len(process.Steps) != 1 || process.Steps[0] != "Decide" {
		t.Errorf("steps = %v, want the step named", process.Steps)
	}
	if impact.RunningInstances != 1 {
		t.Errorf("running instances = %d, want the one still in flight", impact.RunningInstances)
	}

	// Once it finishes, the commitment is no longer in flight.
	h.completeEveryTask(t, instance.ID)
	after, err := h.decisions.DecisionImpact(ctx, stored.ID)
	if err != nil {
		t.Fatalf("impact after completion: %v", err)
	}
	if after.RunningInstances != 0 {
		t.Errorf("running instances = %d after the process finished, want none", after.RunningInstances)
	}
	if len(after.Processes) != 1 {
		t.Error("the process stopped being listed once its instances finished; it still consults the decision")
	}
}

// A decision used only to decide who does the work counts too — the impact view
// would otherwise say "nothing uses this" about a live approval matrix.
func TestTheImpactViewSeesAssignmentTables(t *testing.T) {
	h := newBusinessRuleHarness(t)
	ctx := t.Context()

	matrix := entities.DecisionDefinition{
		Project:   &entities.Project{ID: h.projectID},
		Key:       "who-approves",
		Name:      "Who approves",
		HitPolicy: entities.HitPolicyFirst,
		Outputs:   []entities.DecisionOutput{{ID: "o1", Name: "assignee", Type: "string"}},
		Rules:     []entities.DecisionRule{{ID: "r1", Inputs: []string{}, Outputs: []any{"cfo"}}},
	}
	if _, err := h.decisions.CreateDecision(ctx, matrix); err != nil {
		t.Fatalf("create decision: %v", err)
	}

	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projectID},
		Key:     "approval",
		Name:    "Approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "approve", Type: entities.UserTask, Name: "Approve",
				Properties: map[string]any{handlersimpl.AssignmentDecisionKey: "who-approves"}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "end"},
		},
	}
	if _, err := serviceimpl.NewDefinitionService(h.repo).CreateDefinition(ctx, def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	stored := h.decisionByKey(t, "who-approves")
	impact, err := h.decisions.DecisionImpact(ctx, stored.ID)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if len(impact.Processes) != 1 || impact.Processes[0].DefinitionKey != "approval" {
		t.Errorf("impact = %+v, want the approval process listed", impact.Processes)
	}
}

// The service duplicates the assignment property name rather than importing the
// handlers package, because services must not depend on handlers. If the two
// ever drift, the impact view goes quietly blind to assignment tables.
func TestTheAssignmentPropertyNameIsTheSameOnBothSides(t *testing.T) {
	if serviceimpl.AssignmentDecisionKeyForTest != handlersimpl.AssignmentDecisionKey {
		t.Errorf("the service says %q and the handler says %q",
			serviceimpl.AssignmentDecisionKeyForTest, handlersimpl.AssignmentDecisionKey)
	}
}

// A decision table nobody can test is a spreadsheet with extra steps.
//
// The table is business policy, it changes often, and the person changing it is
// rarely the person who knows every case it was written for — so the cases live
// beside it and are re-run whenever somebody looks.
func TestATableIsRunAgainstItsOwnExamples(t *testing.T) {
	h := newBusinessRuleHarness(t)
	ctx := t.Context()

	id, err := h.decisions.CreateDecision(ctx, entities.DecisionDefinition{
		Project:   &entities.Project{ID: h.projectID},
		Key:       "risk",
		Name:      "Risk band",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "i1", Expression: "amount", Type: "number"}},
		Outputs:   []entities.DecisionOutput{{ID: "o1", Name: "band", Type: "string"}},
		Rules: []entities.DecisionRule{
			{ID: "high", Inputs: []string{">= 1000"}, Outputs: []any{"HIGH"}},
			{ID: "low", Inputs: []string{"-"}, Outputs: []any{"LOW"}},
		},
		Tests: []entities.DecisionTest{
			{ID: "t1", Name: "a large order is high risk",
				Inputs: map[string]any{"amount": 5000.0}, Expected: map[string]any{"band": "HIGH"}},
			{ID: "t2", Name: "a small order is not",
				Inputs: map[string]any{"amount": 5.0}, Expected: map[string]any{"band": "LOW"}},
			{ID: "t3", Name: "somebody's mistaken belief about the threshold",
				Inputs: map[string]any{"amount": 500.0}, Expected: map[string]any{"band": "HIGH"}},
		},
	})
	if err != nil {
		t.Fatalf("create decision: %v", err)
	}

	results, err := h.decisions.RunDecisionTests(ctx, id)
	if err != nil {
		t.Fatalf("run tests: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("ran %d examples, want 3 — the examples must survive being saved", len(results))
	}

	if !results[0].Passed || !results[1].Passed {
		t.Errorf("the two correct examples did not pass: %+v", results[:2])
	}

	failing := results[2]
	if failing.Passed {
		t.Fatal("an example expecting the wrong answer passed")
	}
	// A failure has to show both sides, or the author cannot tell whether the
	// table is wrong or the expectation is.
	if len(failing.Mismatches) != 1 || !strings.Contains(failing.Mismatches[0], "HIGH") || !strings.Contains(failing.Mismatches[0], "LOW") {
		t.Errorf("mismatch = %v, want it to name both what was expected and what happened", failing.Mismatches)
	}
	// And which line decided, which is the first thing anyone looks at.
	if len(failing.MatchedRules) != 1 || failing.MatchedRules[0] != 1 {
		t.Errorf("matched rules = %v, want the catch-all line", failing.MatchedRules)
	}
}

// An example pins the one value it cares about. That is what lets a table grow a
// column without invalidating every example written before it.
func TestAnExampleOnlyChecksWhatItNames(t *testing.T) {
	h := newBusinessRuleHarness(t)
	ctx := t.Context()

	id, err := h.decisions.CreateDecision(ctx, entities.DecisionDefinition{
		Project:   &entities.Project{ID: h.projectID},
		Key:       "risk",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "i1", Expression: "amount", Type: "number"}},
		Outputs: []entities.DecisionOutput{
			{ID: "o1", Name: "band", Type: "string"},
			{ID: "o2", Name: "reviewer", Type: "string"},
		},
		Rules: []entities.DecisionRule{{ID: "r1", Inputs: []string{"-"}, Outputs: []any{"HIGH", "compliance"}}},
		Tests: []entities.DecisionTest{
			{ID: "t1", Name: "only the band matters here",
				Inputs: map[string]any{"amount": 1.0}, Expected: map[string]any{"band": "HIGH"}},
		},
	})
	if err != nil {
		t.Fatalf("create decision: %v", err)
	}

	results, err := h.decisions.RunDecisionTests(ctx, id)
	if err != nil {
		t.Fatalf("run tests: %v", err)
	}
	if !results[0].Passed {
		t.Errorf("an example that named one output failed over another: %v", results[0].Mismatches)
	}
}

// A number typed into a form arrives as a float; a hand-written table may hold
// an int. Comparing those as values rather than as types is the difference
// between a passing test and somebody hunting a bug in their policy.
func TestANumericExpectationIsComparedAsANumber(t *testing.T) {
	h := newBusinessRuleHarness(t)
	ctx := t.Context()

	id, err := h.decisions.CreateDecision(ctx, entities.DecisionDefinition{
		Project:   &entities.Project{ID: h.projectID},
		Key:       "fee",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "i1", Expression: "weight", Type: "number"}},
		Outputs:   []entities.DecisionOutput{{ID: "o1", Name: "fee", Type: "number"}},
		Rules:     []entities.DecisionRule{{ID: "r1", Inputs: []string{"-"}, Outputs: []any{25}}},
		Tests: []entities.DecisionTest{
			{ID: "t1", Name: "the fee is 25",
				Inputs: map[string]any{"weight": 1.0}, Expected: map[string]any{"fee": 25.0}},
		},
	})
	if err != nil {
		t.Fatalf("create decision: %v", err)
	}

	results, err := h.decisions.RunDecisionTests(ctx, id)
	if err != nil {
		t.Fatalf("run tests: %v", err)
	}
	if !results[0].Passed {
		t.Errorf("25 did not equal 25.0: %v", results[0].Mismatches)
	}
}

// A table that cannot be evaluated is a different failure from one that decides
// the wrong thing: the first is broken, the second is a disagreement about
// policy, and they are fixed in different places.
func TestABrokenTableIsReportedAsBrokenNotAsWrong(t *testing.T) {
	h := newBusinessRuleHarness(t)
	ctx := t.Context()

	id, err := h.decisions.CreateDecision(ctx, entities.DecisionDefinition{
		Project:   &entities.Project{ID: h.projectID},
		Key:       "broken",
		HitPolicy: entities.HitPolicyPriority, // ranks by a value list that is not there
		Inputs:    []entities.DecisionInput{{ID: "i1", Expression: "amount", Type: "number"}},
		Outputs:   []entities.DecisionOutput{{ID: "o1", Name: "band", Type: "string"}},
		Rules:     []entities.DecisionRule{{ID: "r1", Inputs: []string{"-"}, Outputs: []any{"HIGH"}}},
		Tests: []entities.DecisionTest{
			{ID: "t1", Name: "anything", Inputs: map[string]any{"amount": 1.0}, Expected: map[string]any{"band": "HIGH"}},
		},
	})
	if err != nil {
		t.Fatalf("create decision: %v", err)
	}

	results, err := h.decisions.RunDecisionTests(ctx, id)
	if err != nil {
		t.Fatalf("run tests: %v", err)
	}
	if results[0].Passed {
		t.Fatal("a table that could not be evaluated reported a passing example")
	}
	if results[0].Err == "" {
		t.Error("a broken table was reported as a wrong answer rather than as broken")
	}
}
