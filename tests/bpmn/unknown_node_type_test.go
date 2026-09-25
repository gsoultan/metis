package bpmn_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	domainadapters "github.com/gsoultan/metis/server/domains/adapters"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A step of a type the engine has no handler for.
//
// The handler factory answered an unknown type with a handler that logged the
// step and returned nil, so the token parked there with nothing coming to move
// it: the instance read as running, raised no incident, and waited for good.
// Deploy accepted the definition, because validation only asked that each step
// have some type, not one the engine knows.
func withUnknownStep(projectID uuid.UUID) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "send-invoice",
		Name:    "Send the invoice",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "send", Type: entities.NodeType("sendTask"), Name: "Send the invoice"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "send"},
			{ID: "f2", SourceRef: "send", TargetRef: "end"},
		},
	}
}

func TestADefinitionWithAStepTheEngineCannotRunIsRefusedAtDeploy(t *testing.T) {
	h := newEngineHarness(t, "Unknown Step Deploy Project")

	_, err := h.svc.CreateDefinition(h.Ctx(), withUnknownStep(h.projID))
	if err == nil {
		t.Fatal("a definition with a step of type sendTask deployed; every instance of it would stop there")
	}
	if !strings.Contains(err.Error(), "send") || !strings.Contains(err.Error(), "sendTask") {
		t.Fatalf("the refusal does not name the step and its type: %v", err)
	}
	// What the caller sent is what was wrong: a 400, not a 500 that spends the
	// error budget the engine's own failures are measured against.
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("the refusal is not marked as the caller's mistake: %v", err)
	}
}

// A version stored before deploy checked the type is still there to start.
// Starting it must fail where the caller sees it, not leave an instance parked
// at a step nothing will ever move.
func TestAStoredVersionWithAStepTheEngineCannotRunFailsToStart(t *testing.T) {
	h := newEngineHarness(t, "Unknown Step Start Project")
	def := withUnknownStep(h.projID)
	def.ID = uuid.New()
	def.Version = 1
	if err := h.repo.Definition().Create(h.Ctx(), domainadapters.DefinitionModelAdapter{Definition: def}.ToModel()); err != nil {
		t.Fatalf("store the version as an older deploy would have: %v", err)
	}

	instanceID, err := h.svc.StartProcess(h.Ctx(), h.projID, def.Key, nil)
	if err == nil {
		instance, readErr := h.svc.GetInstance(h.Ctx(), instanceID)
		if readErr != nil {
			t.Fatalf("start reported success; reading the instance back: %v", readErr)
		}
		t.Fatalf("start reported success; the instance is %s with its token at %v",
			instance.Status, tokenNodes(instance))
	}
	if !strings.Contains(err.Error(), "sendTask") {
		t.Fatalf("the failure does not say which type the engine cannot run: %v", err)
	}
	if got := len(h.instances(t)); got != 0 {
		t.Fatalf("a start that failed left %d instance(s) behind", got)
	}
}

func tokenNodes(instance entities.ProcessInstance) []string {
	nodes := make([]string, 0, len(instance.Tokens))
	for _, token := range instance.Tokens {
		if token.Node != nil {
			nodes = append(nodes, token.Node.ID)
		}
	}
	return nodes
}
