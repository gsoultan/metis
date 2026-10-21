// Replaying a real instance on another version.
//
// The question before a deploy is rarely "what does this version do with
// made-up numbers?". It is "what would last Tuesday's refund have done on it?".
// So a replay reads the case off the instance — what it started with, who did
// each step, what they entered — runs it on the version asked for, and says
// where the two paths part. It reads the instance and changes nothing about it.
package bpmn_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/tests/testutils"
)

// refundApproval asks a person whether to approve; v1 pays whatever they
// approved.
func refundApproval(project uuid.UUID) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: project},
		Key:     "refund",
		Name:    "Refund",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Refund asked for"},
			{ID: "approve", Type: entities.UserTask, Name: "Approve the refund", Properties: map[string]any{
				"form_definition": []any{map[string]any{"id": "approved", "label": "Approved", "type": "boolean"}},
			}},
			{ID: "decide", Type: entities.ExclusiveGateway, Name: "Approved?"},
			{ID: "paid", Type: entities.EndEvent, Name: "Paid"},
			{ID: "refused", Type: entities.EndEvent, Name: "Refused"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "decide"},
			{ID: "yes", SourceRef: "decide", TargetRef: "paid", Condition: "approved = true"},
			{ID: "no", SourceRef: "decide", TargetRef: "refused", Condition: "approved = false"},
		},
	}
}

// refundWithLimit is v2: anything over 100 now goes to the finance lead,
// whatever the approver said.
func refundWithLimit(project uuid.UUID) *entities.ProcessDefinition {
	def := refundApproval(project)
	def.Nodes = []*entities.Node{
		def.Nodes[0], def.Nodes[1],
		{ID: "check", Type: entities.ExclusiveGateway, Name: "Over the limit?"},
		{ID: "finance", Type: entities.UserTask, Name: "Finance lead signs off"},
		{ID: "paid", Type: entities.EndEvent, Name: "Paid"},
	}
	def.Flows = []*entities.SequenceFlow{
		{ID: "f1", SourceRef: "start", TargetRef: "approve"},
		{ID: "f2", SourceRef: "approve", TargetRef: "check"},
		{ID: "over", SourceRef: "check", TargetRef: "finance", Condition: "amount > 100"},
		{ID: "under", SourceRef: "check", TargetRef: "paid", Condition: "amount <= 100"},
		{ID: "f3", SourceRef: "finance", TargetRef: "paid"},
	}
	return def
}

// paidRefund runs a real refund of 120 on v1, approved by sarah, to the end.
func (h *simHarness) paidRefund(t *testing.T) uuid.UUID {
	t.Helper()
	if _, err := h.defs.CreateDefinition(h.ctx, refundApproval(h.projectID)); err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	id, err := h.engine.StartProcess(h.ctx, h.projectID, "refund", map[string]any{"amount": 120})
	if err != nil {
		t.Fatalf("start the real refund: %v", err)
	}
	tasks, err := h.repo.Task().ListByInstance(h.ctx, id)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("the refund's tasks: %v (%d)", err, len(tasks))
	}
	if err := h.tasks.CompleteTask(testutils.AsOperator(h.ctx, "sarah"), uuid.UUID(tasks[0].ID), "sarah",
		map[string]any{"approved": true}); err != nil {
		t.Fatalf("sarah approves: %v", err)
	}
	instance, err := h.engine.GetInstance(h.ctx, id)
	if err != nil || instance.Status != entities.ProcessCompleted {
		t.Fatalf("the real refund is %q (%v), want completed", instance.Status, err)
	}
	return id
}

func (h *simHarness) replayRequest(instance uuid.UUID) entities.SimulationRequest {
	req := h.request()
	req.DefinitionKey = ""
	req.ReplayInstanceID = instance
	return req
}

// The instance's case runs on the version live now, and the run says where it
// leaves the path the instance took.
func TestReplayRunsAnInstancesCaseOnTheLiveVersion(t *testing.T) {
	h := newSimHarness(t)
	instance := h.paidRefund(t)
	if _, err := h.defs.CreateDefinition(h.ctx, refundWithLimit(h.projectID)); err != nil {
		t.Fatalf("deploy v2: %v", err)
	}
	before := h.rowCounts(t)

	run, err := h.simulation.Simulate(h.ctx, h.replayRequest(instance))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}

	if run.Outcome != entities.SimulationOutcomeNeedsAnswer || run.AwaitingNode != "finance" {
		t.Fatalf("outcome %q at %q, want it to stop at finance: on v2 a refund of 120 needs the finance lead",
			run.Outcome, run.AwaitingNode)
	}
	if run.Definition.Version != 2 {
		t.Errorf("ran version %d, want 2, the live one", run.Definition.Version)
	}
	r := run.Replay
	if r == nil {
		t.Fatal("no replay in the run")
	}
	if r.Version != 1 || r.Status != string(entities.ProcessCompleted) {
		t.Errorf("the instance read as version %d, %q; want 1, completed", r.Version, r.Status)
	}
	if r.StartedWith["amount"] != float64(120) && r.StartedWith["amount"] != 120 {
		t.Errorf("started with %v, want amount 120", r.StartedWith)
	}
	approve := slices.IndexFunc(r.Answers, func(a entities.SimulationAnswer) bool { return a.Node == "approve" })
	if approve < 0 || r.Answers[approve].Actor != "sarah" || r.Answers[approve].Returns["approved"] != true {
		t.Fatalf("answers read as %+v, want approve by sarah, approved", r.Answers)
	}
	if !slices.Equal(r.OnlyInRun, []string{"check", "finance"}) {
		t.Errorf("only in the run: %v, want [check finance]", r.OnlyInRun)
	}
	// paid too: the run has not got there. Whether it would is a question for
	// the finance lead's answer, which the instance never needed.
	if !slices.Equal(r.OnlyInInstance, []string{"decide", "paid"}) {
		t.Errorf("only in the instance: %v, want [decide paid]", r.OnlyInInstance)
	}

	// Read, not touched: the real instance and everything else are as they were.
	after := h.rowCounts(t)
	for table, want := range before {
		if got := after[table]; got != want {
			t.Errorf("%s: %d rows before the replay, %d after", table, want, got)
		}
	}
}

// On the version the instance ran on, the replay takes the path it took.
func TestReplayOnItsOwnVersionTakesTheSamePath(t *testing.T) {
	h := newSimHarness(t)
	instance := h.paidRefund(t)

	run, err := h.simulation.Simulate(h.ctx, h.replayRequest(instance))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if run.Outcome != entities.SimulationOutcomeCompleted || run.EndedAtNode != "paid" {
		t.Fatalf("outcome %q at %q, want completed at paid", run.Outcome, run.EndedAtNode)
	}
	if len(run.Replay.OnlyInRun) != 0 || len(run.Replay.OnlyInInstance) != 0 {
		t.Errorf("the same version went another way: only in run %v, only in instance %v",
			run.Replay.OnlyInRun, run.Replay.OnlyInInstance)
	}
}

// What the caller gives wins over what was read, step by step.
func TestReplayLetsTheCallerChangeTheCase(t *testing.T) {
	h := newSimHarness(t)
	instance := h.paidRefund(t)

	req := h.replayRequest(instance)
	req.Answers = []entities.SimulationAnswer{
		{Node: "approve", Kind: entities.AnswerPerson, Actor: "tom", After: "PT1H", Returns: map[string]any{"approved": false}},
	}
	run, err := h.simulation.Simulate(h.ctx, req)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if run.EndedAtNode != "refused" {
		t.Fatalf("ended at %q, want refused: the caller's answer should have replaced sarah's", run.EndedAtNode)
	}
	if !slices.Equal(run.Replay.OnlyInRun, []string{"refused"}) || !slices.Equal(run.Replay.OnlyInInstance, []string{"paid"}) {
		t.Errorf("only in run %v, only in instance %v; want [refused] and [paid]", run.Replay.OnlyInRun, run.Replay.OnlyInInstance)
	}
}

// An instance is replayed only on its own process, and only where the caller
// says it is.
func TestReplayIsRefusedOutsideItsProcessAndProject(t *testing.T) {
	h := newSimHarness(t)
	instance := h.paidRefund(t)

	other := h.replayRequest(instance)
	other.DefinitionKey = "expense-approval"
	if _, err := h.simulation.Simulate(h.ctx, other); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Errorf("replaying a refund as an expense claim: %v, want refused as invalid", err)
	}

	elsewhere := h.replayRequest(instance)
	elsewhere.ProjectID = uuid.New()
	if _, err := h.simulation.Simulate(h.ctx, elsewhere); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("replaying from another project: %v, want not found", err)
	}

	missing := h.replayRequest(uuid.New())
	if _, err := h.simulation.Simulate(h.ctx, missing); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("replaying an instance that does not exist: %v, want not found", err)
	}
}
