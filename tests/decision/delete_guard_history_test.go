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

// A decision is still in use while a version deployed long ago consults it,
// however many versions have been deployed since.
//
// The guard found the processes that consult a decision by reading the
// project's definitions — every version of every process, newest first —
// through a query the store caps at a thousand rows. Once a thousand newer
// versions had been deployed, the version a running instance is still on was
// outside the window: the impact view said nothing used the decision, and the
// delete went through.
func TestADecisionAnOldVersionStillConsultsCannotBeDeleted(t *testing.T) {
	h := newBusinessRuleHarness(t)
	table := entities.DecisionDefinition{
		Key:       "limit",
		Name:      "Spending limit",
		HitPolicy: entities.HitPolicyFirst,
		Inputs:    []entities.DecisionInput{{ID: "i1", Expression: "amount", Type: "number"}},
		Outputs:   []entities.DecisionOutput{{ID: "o1", Name: "band", Type: "string"}},
		Rules:     []entities.DecisionRule{{ID: "r1", Inputs: []string{"-"}, Outputs: []any{"HIGH"}}},
	}
	h.runDecisionThenWait(t, table, map[string]any{"amount": 500.0})
	stored := h.decisionByKey(t, table.Key)

	// Newer versions of an unrelated process, deployed after the one above.
	if err := h.db.WithContext(h.ctx).Exec(`
		INSERT INTO process_definitions (id, created_at, updated_at, project_id, key, name, version, nodes, flows)
		SELECT gen_random_uuid(), now() + interval '1 minute', now() + interval '1 minute', ?, 'unrelated', 'Unrelated', n, '[]', '[]'
		  FROM generate_series(1, ?) AS n`, h.projectID, finishedBehind).Error; err != nil {
		t.Fatalf("seed newer versions: %v", err)
	}

	impact, err := h.decisions.DecisionImpact(h.ctx, stored.ID)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if len(impact.Processes) != 1 || impact.RunningInstances != 1 {
		t.Errorf("the impact view names %d processes and %d running instances; the version deployed before %d newer ones still consults the decision",
			len(impact.Processes), impact.RunningInstances, finishedBehind)
	}
	if err := h.decisions.DeleteDecision(h.ctx, stored.ID); err == nil {
		t.Fatal("the decision was deleted while a version deployed before a thousand newer ones still consults it")
	}
}
