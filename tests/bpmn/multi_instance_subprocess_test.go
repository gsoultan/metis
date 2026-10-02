package bpmn_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A sub-process that runs once per item: the step that repeats is a container,
// and the work of each run happens on the steps inside it.
//
// Entering the sub-process took every token off it — the iteration tokens the
// step had just been given included. A completion with no token to retire is
// refused, so the first run to reach the sub-process's end event was refused,
// and every one after it: the person holding the task inside was told the
// process was no longer waiting for it, for good.

// itemReviewDefinition is start → review each item (a sub-process, once per
// item: start → review → end) → record → end.
func itemReviewDefinition(projID uuid.UUID, key, loop string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projID},
		Key:     key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "sub", Type: entities.SubProcess, Name: "Review each item",
				MultiInstanceType: loop, Collection: "items", ElementVariable: "item"},
			{ID: "s_start", Type: entities.StartEvent, ParentID: "sub"},
			{ID: "review", Type: entities.UserTask, Name: "Review", ParentID: "sub"},
			{ID: "s_end", Type: entities.EndEvent, ParentID: "sub"},
			{ID: "record", Type: entities.UserTask, Name: "Record the outcome"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "sub"},
			{ID: "i1", SourceRef: "s_start", TargetRef: "review"},
			{ID: "i2", SourceRef: "review", TargetRef: "s_end"},
			{ID: "f2", SourceRef: "sub", TargetRef: "record"},
			{ID: "f3", SourceRef: "record", TargetRef: "end"},
		},
	}
}

// startItemReview deploys def and starts one instance over items.
func startItemReview(t *testing.T, h engineHarness, def *entities.ProcessDefinition, items ...any) uuid.UUID {
	t.Helper()
	h.deploy(t, def)
	instanceID, err := h.svc.StartProcess(h.Ctx(), h.projID, def.Key, map[string]any{"items": items})
	if err != nil {
		t.Fatalf("start %s: %v", def.Key, err)
	}
	return instanceID
}

// enteredBeforeTheUpgrade puts the instance back the way the release before
// left one that was inside a repeating sub-process: entering the sub-process
// took every token off it, so the step counts its runs and holds no token.
func enteredBeforeTheUpgrade(ctx context.Context, t *testing.T, h engineHarness, instanceID uuid.UUID, nodeID string) {
	t.Helper()
	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	instance.RemoveTokenByNodeID(nodeID)
	if err := h.engine.UpdateInstance(ctx, instance); err != nil {
		t.Fatalf("stage the tokens the old way: %v", err)
	}
	if left := tokenIterationsOn(ctx, t, h, instanceID, nodeID); len(left) != 0 {
		t.Fatalf("staging failed: the step still holds tokens %v", left)
	}
}

// BPMN 2.0.2 §13.2.7 (Multiple Instances Activity): a multi-instance
// sub-process runs its contents once per item, and completes — producing one
// token — when every run has.
func TestAParallelSubProcessRunOncePerItemFinishesItsInstance(t *testing.T) {
	h := newEngineHarness(t, "Parallel Sub-Process Project")
	ctx := h.Ctx()
	instanceID := startItemReview(t, h, itemReviewDefinition(h.projID, "review-each-item", "parallel"), "a", "b", "c")

	open := openIterationTasks(ctx, t, h, instanceID, "review")
	if len(open) != 3 {
		t.Fatalf("three items were to be reviewed and %d review(s) are open", len(open))
	}
	if held := tokenIterationsOn(ctx, t, h, instanceID, "sub"); !slices.Equal(held, []string{"0", "1", "2"}) {
		t.Fatalf("the sub-process holds tokens %v while its three runs are inside, want one per run", held)
	}
	for done, task := range open {
		if err := completeAs(ctx, h, task, "carol", nil); err != nil {
			t.Fatalf("complete review %d: %v", done, err)
		}
		if left := tokenIterationsOn(ctx, t, h, instanceID, "sub"); len(left) != len(open)-done-1 {
			t.Fatalf("after %d of 3 runs the sub-process still holds tokens %v", done+1, left)
		}
	}
	finishRecording(ctx, t, h, instanceID)
}

// BPMN 2.0.2 §10.3.8 (Loop Characteristics, isSequential) and §13.2.7: the
// runs follow one another, each started when the one before completes.
func TestASequentialSubProcessRunsOneItemAtATimeAndFinishes(t *testing.T) {
	h := newEngineHarness(t, "Sequential Sub-Process Project")
	ctx := h.Ctx()
	instanceID := startItemReview(t, h, itemReviewDefinition(h.projID, "review-each-item-in-turn", "sequential"), "a", "b", "c")

	for _, item := range []string{"a", "b", "c"} {
		open := openIterationTasks(ctx, t, h, instanceID, "review")
		if len(open) != 1 {
			t.Fatalf("at item %s, %d review(s) are open; one at a time means one", item, len(open))
		}
		instance := requireInstanceStatus(ctx, t, h, instanceID, entities.ProcessActive)
		if instance.Variables["item"] != item {
			t.Fatalf("the run under way is for item %v, want %s", instance.Variables["item"], item)
		}
		if err := completeAs(ctx, h, open[0], "carol", nil); err != nil {
			t.Fatalf("complete the review of item %s: %v", item, err)
		}
	}
	if left := tokenIterationsOn(ctx, t, h, instanceID, "sub"); len(left) != 0 {
		t.Fatalf("the finished sub-process still holds tokens %v", left)
	}
	finishRecording(ctx, t, h, instanceID)
}

// BPMN 2.0.2 §13.2.7: the activity completes when all of its instances have.
//
// An instance that was inside the sub-process when the server was upgraded:
// the release before took the step's tokens as it entered, so the step counts
// its runs and holds none. A run that finishes has no token to retire and is
// counted all the same — refusing it would leave the instance where it is for
// ever.
func TestAnInstanceInsideARepeatingSubProcessAtTheUpgradeStillFinishes(t *testing.T) {
	for _, loop := range []string{"parallel", "sequential"} {
		t.Run(loop, func(t *testing.T) {
			h := newEngineHarness(t, "Upgraded Sub-Process Project "+loop)
			ctx := h.Ctx()
			instanceID := startItemReview(t, h, itemReviewDefinition(h.projID, "review-each-item-"+loop, loop), "a", "b", "c")
			enteredBeforeTheUpgrade(ctx, t, h, instanceID, "sub")

			for run := range 3 {
				open := openIterationTasks(ctx, t, h, instanceID, "review")
				if len(open) == 0 {
					t.Fatalf("run %d of 3 has no review open", run+1)
				}
				if err := completeAs(ctx, h, open[0], "carol", nil); err != nil {
					t.Fatalf("complete review %d after the upgrade: %v", run+1, err)
				}
			}
			if left := tokenIterationsOn(ctx, t, h, instanceID, "sub"); len(left) != 0 {
				t.Fatalf("the finished sub-process still holds tokens %v", left)
			}
			finishRecording(ctx, t, h, instanceID)
		})
	}
}

// BPMN 2.0.2 §13.2.7 does not say what a multi-instance activity over an empty
// collection does; this engine runs the step once. For a sub-process that one
// run is inside it, and its end event has to be able to finish the step.
func TestASubProcessGivenNothingToRepeatOverRunsOnceAndMovesOn(t *testing.T) {
	h := newEngineHarness(t, "Empty Sub-Process Project")
	ctx := h.Ctx()
	instanceID := startItemReview(t, h, itemReviewDefinition(h.projID, "review-no-items", "parallel"))

	open := openIterationTasks(ctx, t, h, instanceID, "review")
	if len(open) != 1 {
		t.Fatalf("an empty list opened %d review(s), want the one run", len(open))
	}
	if err := completeAs(ctx, h, open[0], "carol", nil); err != nil {
		t.Fatalf("complete the one run: %v", err)
	}
	if left := tokenIterationsOn(ctx, t, h, instanceID, "sub"); len(left) != 0 {
		t.Fatalf("the finished sub-process still holds tokens %v", left)
	}
	finishRecording(ctx, t, h, instanceID)
}
