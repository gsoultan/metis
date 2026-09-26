package decision_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
)

// finishedBehind is the store's default limit on one read: storm starts every
// query with a limit of a thousand rows.
const finishedBehind = 1000

// A decision that a running instance still reaches cannot be deleted.
//
// The guard counted the running instances of every process that consults the
// decision by reading the process's instances — finished ones included, newest
// first — through a query the store caps at a thousand rows. A running
// instance started before a thousand others finished was not in the window, so
// it did not count: the impact view said nothing was in flight, and the delete
// went through, leaving that instance to fail at a step that worked yesterday.
func TestADecisionAnOldRunningInstanceStillReachesCannotBeDeleted(t *testing.T) {
	h := newBusinessRuleHarness(t)
	table := entities.DecisionDefinition{
		Key:       "limit",
		Name:      "Spending limit",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "i1", Expression: "amount", Type: "number"}},
		Outputs:   []entities.DecisionOutput{{ID: "o1", Name: "band", Type: "string"}},
		Rules:     []entities.DecisionRule{{ID: "r1", Inputs: []string{"-"}, Outputs: []any{"HIGH"}}},
	}
	running := h.runDecisionThenWait(t, table, map[string]any{"amount": 500.0})
	stored := h.decisionByKey(t, table.Key)

	for range finishedBehind {
		if _, err := h.repo.Process().Create(h.ctx, models.ProcessInstanceModel{
			ProjectID:    models.UUID(h.projectID),
			DefinitionID: models.UUID(running.Definition.ID),
			Status:       models.ProcessCompleted,
		}); err != nil {
			t.Fatalf("record a finished instance: %v", err)
		}
	}

	impact, err := h.decisions.DecisionImpact(h.ctx, stored.ID)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if impact.RunningInstances != 1 {
		t.Errorf("the impact view counts %d running instances; the one started before %d finished ones is still running",
			impact.RunningInstances, finishedBehind)
	}
	if err := h.decisions.DeleteDecision(h.ctx, stored.ID); err == nil {
		t.Fatal("the decision was deleted while a running instance still reaches it")
	}
}
