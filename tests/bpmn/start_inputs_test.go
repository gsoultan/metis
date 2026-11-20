package bpmn_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// Start inputs.
//
// A start step declares what a run must be started with. A start that leaves
// a required input out, or gives one the wrong kind of value, is refused
// whichever way it arrives, and starts nothing.

// claimInputs is a form as the designer saves it: an amount that must be
// given, and a currency that falls back to pounds.
func claimInputs() []any {
	return []any{
		map[string]any{"id": "amount", "type": "number", "label": "Amount", "required": true},
		map[string]any{"id": "currency", "type": "select", "defaultValue": "GBP", "options": []any{
			map[string]any{"value": "GBP", "label": "Pounds"}, map[string]any{"value": "EUR", "label": "Euros"},
		}},
	}
}

// expenseClaim has three ways in, each a start step declaring the same
// inputs: by hand, on a message, on a signal.
func expenseClaim(projectID uuid.UUID) *entities.ProcessDefinition {
	withInputs := func(more map[string]any) map[string]any {
		properties := map[string]any{entities.StartFormProperty: claimInputs()}
		for name, value := range more {
			properties[name] = value
		}
		return properties
	}
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID}, Key: "expense-claim",
		Nodes: []*entities.Node{
			{ID: "byHand", Type: entities.StartEvent, Properties: withInputs(nil)},
			{ID: "onMessage", Type: entities.StartEvent, Properties: withInputs(map[string]any{"message_name": "claim-submitted"})},
			{ID: "onSignal", Type: entities.StartEvent, Properties: withInputs(map[string]any{"signal_name": "claims-opened"})},
			{ID: "review", Type: entities.UserTask, Name: "Review the claim"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "byHand", TargetRef: "review"},
			{ID: "f2", SourceRef: "onMessage", TargetRef: "review"},
			{ID: "f3", SourceRef: "onSignal", TargetRef: "review"},
			{ID: "f4", SourceRef: "review", TargetRef: "end"},
		},
	}
}

func TestAStartMissingARequiredInputIsRefusedHoweverItArrives(t *testing.T) {
	h := newEngineHarness(t, "Start Inputs Refused Project")
	h.deploy(t, expenseClaim(h.projID))

	wrong := map[string]any{"currency": "USD"}
	for _, way := range []struct {
		name  string
		start func() error
	}{
		{"the API", func() error {
			_, err := h.engine.StartProcess(h.Ctx(), h.projID, "expense-claim", wrong)
			return err
		}},
		{"a message", func() error {
			_, err := h.engine.StartFromMessage(h.Ctx(), h.projID, "claim-submitted", wrong)
			return err
		}},
		{"a signal", func() error {
			return h.engine.BroadcastSignal(h.Ctx(), h.projID, "claims-opened", wrong)
		}},
	} {
		err := way.start()
		if !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Fatalf("%s: a start with no amount answered %v, want it refused as invalid", way.name, err)
		}
		for _, said := range []string{"amount is required", "currency must be one of GBP, EUR"} {
			if !strings.Contains(err.Error(), said) {
				t.Errorf("%s: the refusal does not say %q: %v", way.name, said, err)
			}
		}
	}
	if got := len(h.instances(t)); got != 0 {
		t.Fatalf("refused starts left %d runs behind", got)
	}
}

func TestAStartThatGivesItsInputsRunsWithTheDefaultsOfTheRest(t *testing.T) {
	h := newEngineHarness(t, "Start Inputs Admitted Project")
	h.deploy(t, expenseClaim(h.projID))

	byHand, err := h.engine.StartProcess(h.Ctx(), h.projID, "expense-claim", map[string]any{"amount": 42, "note": "taxi"})
	if err != nil {
		t.Fatalf("a start that gave the amount was refused: %v", err)
	}
	if started, err := h.engine.StartFromMessage(h.Ctx(), h.projID, "claim-submitted", map[string]any{"amount": 7.5, "currency": "EUR"}); err != nil || started != 1 {
		t.Fatalf("a message that gave the amount started %d runs: %v", started, err)
	}

	run, err := h.engine.GetInstance(h.Ctx(), byHand)
	if err != nil {
		t.Fatal(err)
	}
	if run.Variables["currency"] != "GBP" {
		t.Errorf("the currency left out is %v, want its default GBP", run.Variables["currency"])
	}
	if run.Variables["note"] != "taxi" {
		t.Errorf("a variable the start step does not declare was lost: %v", run.Variables)
	}
	if got := len(h.instances(t)); got != 2 {
		t.Fatalf("two admitted starts left %d runs", got)
	}
}

// A run another run starts is given what its parent's step maps to it. The
// parent is already running and cannot be asked again, so the child's inputs
// are not held against it.
func TestARunStartedByAnotherRunIsNotHeldToTheInputs(t *testing.T) {
	h := newEngineHarness(t, "Start Inputs Child Project")
	h.deploy(t, expenseClaim(h.projID))

	parent, err := h.engine.StartProcess(h.Ctx(), h.projID, "expense-claim", map[string]any{"amount": 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.engine.StartSubProcess(h.Ctx(), h.projID, "expense-claim", 0, nil, parent, "review"); err != nil {
		t.Fatalf("a child run was refused for its inputs: %v", err)
	}
}

func TestAScheduleWhoseRequiredInputHasNoDefaultIsRefusedAtDeploy(t *testing.T) {
	h := newEngineHarness(t, "Start Inputs Schedule Project")

	def := nightlyReport(h.projID, everyTenMinutes())
	scheduled := def.FindNode("onSchedule")
	scheduled.Properties[entities.StartFormProperty] = claimInputs()
	_, err := h.svc.CreateDefinition(h.Ctx(), def)
	if err == nil || !strings.Contains(err.Error(), `required input "amount" has no default`) {
		t.Fatalf("a schedule nobody could fill the inputs of deployed: %v", err)
	}

	// With a default, it deploys, and the scheduled run starts with it.
	inputs := claimInputs()
	inputs[0].(map[string]any)["defaultValue"] = 100
	def = nightlyReport(h.projID, everyTenMinutes())
	def.FindNode("onSchedule").Properties[entities.StartFormProperty] = inputs
	h.deploy(t, def)

	if got := h.runSchedules(t, time.Now().Add(11*time.Minute)); got != 1 {
		t.Fatalf("the schedule started %d runs, want 1", got)
	}
	runs := h.instances(t)
	if len(runs) != 1 {
		t.Fatalf("expected one run, got %d", len(runs))
	}
	run, err := h.engine.GetInstance(h.Ctx(), uuid.UUID(runs[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	if amount, ok := run.Variables["amount"].(float64); !ok || amount != 100 || run.Variables["currency"] != "GBP" {
		t.Fatalf("the scheduled run did not start with its inputs' defaults: %v", run.Variables)
	}
}
