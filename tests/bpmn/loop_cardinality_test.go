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

// A step set to repeat a fixed number of times far beyond anything a person
// could work through.
//
// The engine creates every parallel token before any of them runs, so a
// definition saying <loopCardinality>2000000000</loopCardinality> exhausted
// the server's memory on the first instance that reached the step.
func withCardinality(projectID uuid.UUID, n int) *entities.ProcessDefinition {
	def := withLoop(projectID, "parallel")
	def.Key = "check-many-references"
	def.Nodes[1].LoopCardinality = n
	return def
}

func TestAStepThatRepeatsTooManyTimesIsRefusedAtDeploy(t *testing.T) {
	h := newEngineHarness(t, "Loop Cardinality Deploy Project")

	_, err := h.svc.CreateDefinition(h.Ctx(), withCardinality(h.projID, 2_000_000_000))
	if err == nil {
		t.Fatal("a step that repeats two billion times deployed; the first instance to reach it exhausts memory")
	}
	if !strings.Contains(err.Error(), "check") || !strings.Contains(err.Error(), "2000000000") {
		t.Fatalf("the refusal does not name the step and the count: %v", err)
	}
	if !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Fatalf("the refusal is not marked as the caller's mistake: %v", err)
	}

	def := withCardinality(h.projID, entities.MaxLoopCardinality)
	def.Key += "-at-the-limit"
	if _, err := h.svc.CreateDefinition(h.Ctx(), def); err != nil {
		t.Errorf("a step that repeats exactly the limit was refused: %v", err)
	}
}

func TestAStoredVersionThatRepeatsTooManyTimesFailsToStart(t *testing.T) {
	h := newEngineHarness(t, "Loop Cardinality Start Project")
	def := withCardinality(h.projID, 2_000_000_000)
	def.ID = uuid.New()
	def.Version = 1
	if err := h.repo.Definition().Create(h.Ctx(), domainadapters.DefinitionModelAdapter{Definition: def}.ToModel()); err != nil {
		t.Fatalf("store the version as an older deploy would have: %v", err)
	}

	_, err := h.svc.StartProcess(h.Ctx(), h.projID, def.Key, nil)
	if err == nil {
		t.Fatal("start reported success for a step that repeats two billion times")
	}
	if !strings.Contains(err.Error(), "2000000000") {
		t.Fatalf("the failure does not say the count is the problem: %v", err)
	}
	if got := len(h.instances(t)); got != 0 {
		t.Fatalf("a start that failed left %d instance(s) behind", got)
	}
}
