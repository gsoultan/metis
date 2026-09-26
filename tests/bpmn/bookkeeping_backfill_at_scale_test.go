package bpmn_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	service_impl2 "github.com/gsoultan/metis/server/domains/services/impl"
)

// finishedSince is as many instances as one read of the generated store
// returns: storm starts every query with a limit of 1000.
const finishedSince = 1000

// The bookkeeping upgrade moves the progress of every running instance, not
// only of those among the newest thousand.
//
// It runs once, as migration 3. It read every instance of every state, newest
// first, through a query the store caps at a thousand rows, and then kept the
// running ones — so an instance still running behind a thousand newer ones,
// finished or not, was never moved. Its multi-instance progress stayed in the
// variables, where nothing reads it any more: its iterations would start again
// from zero.
func TestTheBookkeepingUpgradeReachesARunningInstanceBehindAThousandNewerOnes(t *testing.T) {
	h := newEngineHarness(t, "Legacy Bookkeeping At Scale Project")
	ctx := h.Ctx()
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "review-legacy-many",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "review", Type: entities.UserTask, Name: "Review the item", MultiInstanceType: "parallel", Collection: "items"},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "review"},
			{ID: "f2", SourceRef: "review", TargetRef: "end"},
		},
	})
	instanceID, err := h.svc.StartProcess(ctx, h.projID, "review-legacy-many", map[string]any{"items": []any{"a", "b", "c"}})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}
	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	instance.MultiInstance = nil
	instance.SetVariable("_mi_review_active", true)
	instance.SetVariable("_mi_review_total", 3)
	instance.SetVariable("_mi_review_completed", 2)
	if err := h.engine.UpdateInstance(ctx, instance); err != nil {
		t.Fatalf("stage the legacy shape: %v", err)
	}

	if err := h.db.WithContext(ctx).Exec(`
		INSERT INTO process_instances (id, created_at, updated_at, project_id, definition_id, status, variables,
		                               tokens, completed_nodes, compensated_nodes, multi_instance, joins)
		SELECT gen_random_uuid(), now() + interval '1 minute', now() + interval '1 minute', ?,
		       (SELECT definition_id FROM process_instances WHERE id = ?), 'completed', '',
		       '[]', '[]', '[]', '{}', '{}'
		  FROM generate_series(1, ?) AS n`, h.projID, instanceID, finishedSince).Error; err != nil {
		t.Fatalf("seed %d newer instances: %v", finishedSince, err)
	}

	result, err := service_impl2.BackfillEngineBookkeeping(ctx, h.repo)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if result.Migrated != 1 {
		t.Errorf("the upgrade migrated %d instances; the one running behind %d newer ones was not among them",
			result.Migrated, finishedSince)
	}
	migrated, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload after backfill: %v", err)
	}
	if completed, total, ok := migrated.MultiInstanceProgress("review"); !ok || completed != 2 || total != 3 {
		t.Fatalf("after the upgrade the instance's progress reads %d of %d (recorded: %v); want 2 of 3", completed, total, ok)
	}
}
