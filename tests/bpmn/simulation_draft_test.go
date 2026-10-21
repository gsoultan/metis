// Simulating a draft: the model on the canvas, before anybody deploys it.
//
// Until this, testing a change meant deploying it, and deploying it made it the
// version new instances start on. A draft run has to keep every promise the
// deployed run keeps — nothing persists, nothing escapes — and one more: the
// draft never becomes a version. Not a staged one, not a live one, not a gap in
// the numbering.
package bpmn_test

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// shortcutDraft is the harness's expense process with the card charge taken
// out: approve, then straight to a different end.
//
// Chosen so the deployed and the draft model cannot be confused in a trace. The
// deployed one stops to ask how the charge went; this one finishes.
func shortcutDraft() *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Key:  "expense-approval",
		Name: "Expense Approval (draft)",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Expense submitted"},
			{ID: "approve", Type: entities.UserTask, Name: "Approve the expense"},
			{ID: "end-draft", Type: entities.EndEvent, Name: "Approved without a charge"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "end-draft"},
		},
	}
}

func approveAnswer() []entities.SimulationAnswer {
	return []entities.SimulationAnswer{
		{Node: "approve", Kind: entities.AnswerPerson, Actor: "sarah", After: "PT1H"},
	}
}

func (h *simHarness) definitionCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := h.db.Table("process_definitions").Count(&n).Error; err != nil {
		t.Fatalf("counting process_definitions: %v", err)
	}
	return n
}

// The draft is what runs — not the deployed version under the same key.
func TestSimulationRunsTheDraftNotTheDeployedVersion(t *testing.T) {
	h := newSimHarness(t)

	req := h.request()
	req.Draft = shortcutDraft()
	req.Answers = approveAnswer()

	run, err := h.simulation.Simulate(h.ctx, req)
	if err != nil {
		t.Fatalf("simulate the draft: %v", err)
	}
	if run.Outcome != entities.SimulationOutcomeCompleted || run.EndedAtNode != "end-draft" {
		t.Fatalf("outcome %q at %q, want completed at end-draft — the deployed model ran instead of the draft",
			run.Outcome, run.EndedAtNode)
	}
	if !run.Definition.Draft || run.Definition.Version != 0 {
		t.Errorf("definition reported as %+v, want draft with no version: the version it ran as was rolled back",
			run.Definition)
	}
}

// The draft never becomes a version, and nothing else it touched persists.
func TestSimulatingADraftPersistsNothing(t *testing.T) {
	h := newSimHarness(t)
	before := h.rowCounts(t)
	definitionsBefore := h.definitionCount(t)

	req := h.request()
	req.Draft = shortcutDraft()
	req.Answers = approveAnswer()
	if _, err := h.simulation.Simulate(h.ctx, req); err != nil {
		t.Fatalf("simulate the draft: %v", err)
	}

	if got := h.definitionCount(t); got != definitionsBefore {
		t.Fatalf("%d definitions before, %d after — the draft was saved as a version", definitionsBefore, got)
	}
	after := h.rowCounts(t)
	for table, want := range before {
		if got := after[table]; got != want {
			t.Errorf("%s: %d rows before the draft run, %d after", table, want, got)
		}
	}

	// And the deployed model is still the one that runs by default.
	deployed, err := h.simulation.Simulate(h.ctx, func() entities.SimulationRequest {
		r := h.request()
		r.Answers = approveAnswer()
		return r
	}())
	if err != nil {
		t.Fatalf("simulate the deployed version: %v", err)
	}
	if deployed.AwaitingNode != "charge" {
		t.Errorf("deployed run stopped at %q, want charge: the draft leaked into what is live", deployed.AwaitingNode)
	}
}

// A process nobody has deployed yet can be simulated: the first draft is the
// case that most needs it.
func TestSimulatingANeverDeployedDraft(t *testing.T) {
	h := newSimHarness(t)

	draft := shortcutDraft()
	draft.Key = "brand-new"
	req := h.request()
	req.DefinitionKey = ""
	req.Draft = draft
	req.Answers = approveAnswer()

	run, err := h.simulation.Simulate(h.ctx, req)
	if err != nil {
		t.Fatalf("simulate a draft with no deployed version: %v", err)
	}
	if run.Outcome != entities.SimulationOutcomeCompleted || run.Definition.Key != "brand-new" {
		t.Fatalf("outcome %q for key %q, want completed for brand-new", run.Outcome, run.Definition.Key)
	}
}

// A .bpmn file runs the same way, which is what a pipeline holds.
func TestSimulatingADraftFromBPMNXML(t *testing.T) {
	h := newSimHarness(t)

	req := h.request()
	req.DraftBPMN = `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="http://www.omg.org/spec/BPMN/20100524/MODEL">
  <process id="expense-approval" isExecutable="true">
    <startEvent id="start"><outgoing>f1</outgoing></startEvent>
    <userTask id="approve" name="Approve the expense"><incoming>f1</incoming><outgoing>f2</outgoing></userTask>
    <endEvent id="end-draft"><incoming>f2</incoming></endEvent>
    <sequenceFlow id="f1" sourceRef="start" targetRef="approve"/>
    <sequenceFlow id="f2" sourceRef="approve" targetRef="end-draft"/>
  </process>
</definitions>`
	req.Answers = approveAnswer()

	run, err := h.simulation.Simulate(h.ctx, req)
	if err != nil {
		t.Fatalf("simulate a BPMN draft: %v", err)
	}
	if run.EndedAtNode != "end-draft" {
		t.Fatalf("ended at %q, want end-draft", run.EndedAtNode)
	}
}

// A draft a deploy would refuse is refused here too, in the deploy's words,
// rather than run in a shape production would never accept.
func TestSimulationRefusesADraftThatCouldNotDeploy(t *testing.T) {
	tests := []struct {
		name  string
		draft func() *entities.ProcessDefinition
		want  string
	}{
		{
			name: "a step the engine cannot run",
			draft: func() *entities.ProcessDefinition {
				d := shortcutDraft()
				d.Nodes[1].Type = ""
				return d
			},
			want: "could not be deployed",
		},
		{
			name: "a key that disagrees with the request",
			draft: func() *entities.ProcessDefinition {
				d := shortcutDraft()
				d.Key = "something-else"
				return d
			},
			want: "names",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newSimHarness(t)
			definitionsBefore := h.definitionCount(t)

			req := h.request()
			req.Draft = tc.draft()
			_, err := h.simulation.Simulate(h.ctx, req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want a refusal mentioning %q", err, tc.want)
			}
			if got := h.definitionCount(t); got != definitionsBefore {
				t.Errorf("a refused draft changed the definition count from %d to %d", definitionsBefore, got)
			}
		})
	}
}
