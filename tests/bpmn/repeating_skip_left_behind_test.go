package bpmn_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/models"
)

// What an earlier release's skip of a repeating approval left: every open
// task of the step withdrawn, one run counted, and the other runs' tokens on
// the step with no task under them — on the version it was on, because the
// rewrite then passed the instance over. The same migration run once more now
// ends the step whole: the tokens and the count of runs go, the instance moves
// on once and is migrated.
//
// No release since leaves an instance so. It is put there by the two things
// that release's skip did, in its order: the step's open tasks are withdrawn,
// and the engine is asked to advance past the step as though one run had
// finished.
func TestRunningTheMigrationAgainClearsAnInstanceAnEarlierSkipLeftPartSkipped(t *testing.T) {
	h := newEngineHarness(t, "Part Skipped Project")
	ctx := h.Ctx()
	v1def := withFlowsListed(approvalDefinition(h.projID, "part-skipped", "parallel", ""))
	id := startApproval(t, h, v1def, "ana", "budi", "citra")
	row, err := h.repo.Process().Get(ctx, id)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	v1 := uuid.UUID(row.DefinitionID)

	for _, task := range openIterationTasks(ctx, t, h, id, "approve") {
		if err := h.repo.Task().UpdateStatus(ctx, task.ID, models.TaskCanceled); err != nil {
			t.Fatalf("withdraw a run's task, as the earlier skip did: %v", err)
		}
	}
	def, err := h.engine.GetProcessDefinition(ctx, v1)
	if err != nil {
		t.Fatalf("read the version: %v", err)
	}
	live, err := h.engine.GetInstance(ctx, id)
	if err != nil {
		t.Fatalf("read the instance: %v", err)
	}
	if err := h.engine.Proceed(ctx, &live, def, "approve"); err != nil {
		t.Fatalf("advance past the step as though one run had finished, as the earlier skip did: %v", err)
	}
	left := requireInstanceStatus(ctx, t, h, id, entities.ProcessActive)
	if on := tokensOn(t, h, id, "approve"); on != 2 || len(openIterationTasks(ctx, t, h, id, "approve")) != 0 || !left.IsMultiInstanceActive("approve") {
		t.Fatalf("the step holds %d token(s) and %d open task(s), counting runs: %v; this test needs two tokens, no task and the count still open",
			on, len(openIterationTasks(ctx, t, h, id, "approve")), left.IsMultiInstanceActive("approve"))
	}

	v2def := withFlowsListed(approvalDefinition(h.projID, "part-skipped", "parallel", ""))
	v2, err := h.svc.CreateDefinition(ctx, v2def)
	if err != nil {
		t.Fatalf("deploy the next version: %v", err)
	}
	skip := []servicecontracts.MigrationOption{
		servicecontracts.WithNodeActions(map[string]servicecontracts.NodeAction{
			"approve": {Kind: servicecontracts.NodeActionSkip, Reason: "the purchase was approved by the board instead"},
		}),
		servicecontracts.WithActor("dita"),
	}
	if err := migrateSeconded(t, h, v1, v2, nil, skip...); err != nil {
		t.Fatalf("the same migration, run once more: %v", err)
	}

	now := requireInstanceStatus(ctx, t, h, id, entities.ProcessActive)
	after, err := h.repo.Process().Get(ctx, id)
	if err != nil || uuid.UUID(after.DefinitionID) != v2 {
		t.Fatalf("the instance is on version %s (err %v), want it migrated to %s", uuid.UUID(after.DefinitionID), err, v2)
	}
	if tokensOn(t, h, id, "approve") != 0 || now.IsMultiInstanceActive("approve") || len(now.Tokens) != 1 || !h.waitingAt(ctx, t, id, "record") {
		t.Fatalf("after the migration: %d token(s) on the step, counting runs: %v, %d token(s) in all; want the step ended whole and the instance at the next step once",
			tokensOn(t, h, id, "approve"), now.IsMultiInstanceActive("approve"), len(now.Tokens))
	}
}
