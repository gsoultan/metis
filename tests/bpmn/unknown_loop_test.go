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

// A step that repeats in a way the engine does not know.
//
// The engine runs a step more than once in parallel or in sequence. For any
// other value it marked the loop started, took the token off the step, and
// then started neither kind: nothing ran, nothing would finish the loop, and
// the instance waited for good. Deploy accepted the value, because nothing
// checked it.
func withLoop(projectID uuid.UUID, loop string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "check-references",
		Name:    "Check each reference",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "check", Type: entities.UserTask, Name: "Check a reference", Assignee: "ada",
				MultiInstanceType: loop, LoopCardinality: 3},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "check"},
			{ID: "f2", SourceRef: "check", TargetRef: "end"},
		},
	}
}

func TestAStepThatRepeatsInAWayTheEngineDoesNotKnowIsRefusedAtDeploy(t *testing.T) {
	h := newEngineHarness(t, "Unknown Loop Deploy Project")

	_, err := h.svc.CreateDefinition(h.Ctx(), withLoop(h.projID, "Parallel"))
	if err == nil {
		t.Fatal("a step that repeats \"Parallel\" deployed; every instance of it would stop there")
	}
	if !strings.Contains(err.Error(), "check") || !strings.Contains(err.Error(), "Parallel") {
		t.Fatalf("the refusal does not name the step and the value: %v", err)
	}
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("the refusal is not marked as the caller's mistake: %v", err)
	}

	for _, loop := range []string{"", "none", "parallel", "sequential"} {
		def := withLoop(h.projID, loop)
		def.Key += "-" + loop
		if _, err := h.svc.CreateDefinition(h.Ctx(), def); err != nil {
			t.Errorf("a step that repeats %q was refused: %v", loop, err)
		}
	}
}

func TestAStoredVersionThatRepeatsInAWayTheEngineDoesNotKnowFailsToStart(t *testing.T) {
	h := newEngineHarness(t, "Unknown Loop Start Project")
	def := withLoop(h.projID, "standard")
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
		t.Fatalf("start reported success; the instance is %s with its tokens at %v",
			instance.Status, tokenNodes(instance))
	}
	if !strings.Contains(err.Error(), "standard") {
		t.Fatalf("the failure does not say which kind of repeat the engine cannot run: %v", err)
	}
	if got := len(h.instances(t)); got != 0 {
		t.Fatalf("a start that failed left %d instance(s) behind", got)
	}
}
