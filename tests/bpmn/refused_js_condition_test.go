package bpmn_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/internal/pkg/features"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

// The javascript-conditions flag ships off, which means a gateway routed
// entirely by `js:` conditions can no longer select a flow. What the engine
// does at that moment is the whole safety argument for turning the flag off:
// per AGENTS.md §0, a gateway that cannot choose must raise an incident, never
// fall through to an arbitrary branch.
//
// These tests pin both halves — the loud failure, and the one case where
// carrying on is correct because the author declared where to go.

func jsGateway(projectID uuid.UUID, key, defaultFlow string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     key,
		Name:    "Refused JavaScript condition",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: "choose", Type: entities.ExclusiveGateway, DefaultFlow: defaultFlow,
				Incoming: []string{"f1"}, Outgoing: []string{"approve", "reject"}},
			{ID: "approved", Type: entities.EndEvent, Incoming: []string{"approve"}},
			{ID: "rejected", Type: entities.EndEvent, Incoming: []string{"reject"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "choose"},
			{ID: "approve", SourceRef: "choose", TargetRef: "approved", Condition: "js:amount > 100"},
			{ID: "reject", SourceRef: "choose", TargetRef: "rejected", Condition: "js:amount <= 100"},
		},
	}
}

// TestRefusedJavaScriptConditionRaisesRatherThanGuessing is the load-bearing
// case. With every branch condition refused and no default flow declared, the
// engine has no basis on which to route the token. Taking flows[0] would send
// a real approval down a branch nobody chose, so it must refuse instead — and
// say which gateway, so the incident names the thing to fix.
func TestRefusedJavaScriptConditionRaisesRatherThanGuessing(t *testing.T) {
	defer features.OverrideForTest(features.JavaScriptConditions, false)()

	h := newEngineHarness(t, "Refused JS")
	ctx := t.Context()

	if _, err := h.svc.CreateDefinition(ctx, jsGateway(h.projID, "refused-js", "")); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	_, err := h.svc.StartProcess(ctx, h.projID, "refused-js", map[string]any{"amount": 500})
	if err == nil {
		t.Fatal("the gateway routed a token with every condition refused; it must raise instead of guessing")
	}
	if !strings.Contains(err.Error(), "choose") {
		t.Errorf("the failure does not name the gateway that could not decide: %v", err)
	}
}

// TestRefusedJavaScriptConditionStillHonoursADeclaredDefault is the other half.
// A default flow is the author saying where to go when nothing matches, so
// taking it is correct BPMN semantics rather than a guess — and every refusal
// is logged at error level naming the condition, so the migration still has a
// worklist rather than a silent behaviour change.
func TestRefusedJavaScriptConditionStillHonoursADeclaredDefault(t *testing.T) {
	defer features.OverrideForTest(features.JavaScriptConditions, false)()

	h := newEngineHarness(t, "Refused JS Default")
	ctx := t.Context()

	if _, err := h.svc.CreateDefinition(ctx, jsGateway(h.projID, "refused-js-default", "reject")); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "refused-js-default", map[string]any{"amount": 500})
	if err != nil {
		t.Fatalf("a declared default flow should still route: %v", err)
	}

	inst, err := h.svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if string(inst.Status) != "completed" {
		t.Fatalf("instance is %q, want completed via the default flow", inst.Status)
	}
}

// TestJavaScriptConditionRoutesWhenExplicitlyEnabled proves the escape hatch an
// installation mid-migration depends on: with the flag on, the same definition
// routes by its conditions exactly as before.
func TestJavaScriptConditionRoutesWhenExplicitlyEnabled(t *testing.T) {
	defer features.OverrideForTest(features.JavaScriptConditions, true)()

	h := newEngineHarness(t, "Enabled JS")
	ctx := t.Context()

	if _, err := h.svc.CreateDefinition(ctx, jsGateway(h.projID, "enabled-js", "")); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "enabled-js", map[string]any{"amount": 500})
	if err != nil {
		t.Fatalf("start with JavaScript conditions enabled: %v", err)
	}
	inst, err := h.svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if string(inst.Status) != "completed" {
		t.Fatalf("instance is %q, want completed", inst.Status)
	}
}
