package bpmn_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// Migrating running instances onto another version is refused whenever the move
// would leave work somewhere the target version cannot execute.
//
// Every one of these was previously accepted and applied. The worst of them —
// a token left on a node the target does not have — then hung the request
// forever the next time anybody tried to advance it.
func TestMigrationRefusesAMoveItCannotLand(t *testing.T) {
	t.Run("a mapping naming a node the target does not have", func(t *testing.T) {
		svc, projectID, ctx := releaseFixture(t)
		v1 := approvalModel(projectID, "v1", "hold")
		v1ID, err := svc.CreateDefinition(ctx, &v1)
		if err != nil {
			t.Fatalf("deploy v1: %v", err)
		}
		if _, err := svc.StartProcess(ctx, projectID, "expense-approval", nil); err != nil {
			t.Fatalf("start: %v", err)
		}
		v2 := approvalModel(projectID, "v2", "approve")
		v2ID, err := svc.CreateDefinition(ctx, &v2)
		if err != nil {
			t.Fatalf("deploy v2: %v", err)
		}

		err = svc.MigrateInstances(ctx, v1ID, v2ID, map[string]string{"hold": "does-not-exist"})
		if err == nil {
			t.Fatal("a mapping to a node the target lacks should be refused")
		}
		if !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Fatalf("expected an invalid-argument refusal, got %v", err)
		}
		if !strings.Contains(err.Error(), "does-not-exist") {
			t.Fatalf("the refusal should name the bad target, got %q", err)
		}
	})

	t.Run("an empty mapping that would strand a token", func(t *testing.T) {
		svc, projectID, ctx := releaseFixture(t)
		v1 := approvalModel(projectID, "v1", "hold")
		v1ID, err := svc.CreateDefinition(ctx, &v1)
		if err != nil {
			t.Fatalf("deploy v1: %v", err)
		}
		instanceID, err := svc.StartProcess(ctx, projectID, "expense-approval", nil)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		// v2 renames the node the instance is parked on, and nothing maps it.
		v2 := approvalModel(projectID, "v2", "approve")
		v2ID, err := svc.CreateDefinition(ctx, &v2)
		if err != nil {
			t.Fatalf("deploy v2: %v", err)
		}

		err = svc.MigrateInstances(ctx, v1ID, v2ID, map[string]string{})
		if err == nil {
			t.Fatal("stranding a token on a node the target lacks should be refused")
		}
		if !strings.Contains(err.Error(), "hold") {
			t.Fatalf("the refusal should name the node nothing covers, got %q", err)
		}

		// Refused means untouched: the instance is still on v1 and still works.
		instance, err := svc.GetInstance(ctx, instanceID)
		if err != nil {
			t.Fatalf("get instance: %v", err)
		}
		if instance.Definition.ID != v1ID {
			t.Fatalf("a refused migration must not move the instance, it is on %s", instance.Definition.ID)
		}
		tasks, err := svc.ListTasks(ctx, projectID)
		if err != nil {
			t.Fatalf("list tasks: %v", err)
		}
		for _, task := range tasks {
			if task.Instance != nil && task.Instance.ID == instanceID {
				if err := svc.CompleteTask(ctx, task.ID, "approver", nil); err != nil {
					t.Fatalf("the instance should still be completable on v1: %v", err)
				}
			}
		}
	})

	t.Run("two different processes", func(t *testing.T) {
		svc, projectID, ctx := releaseFixture(t)
		v1 := approvalModel(projectID, "v1", "hold")
		v1ID, err := svc.CreateDefinition(ctx, &v1)
		if err != nil {
			t.Fatalf("deploy v1: %v", err)
		}
		other := entities.ProcessDefinition{
			Project: &entities.Project{ID: projectID},
			Key:     "supplier-onboarding",
			Name:    "Other",
			Nodes: []*entities.Node{
				{ID: "start", Type: entities.StartEvent},
				{ID: "hold", Type: entities.UserTask, Assignee: "approver"},
				{ID: "end", Type: entities.EndEvent},
			},
			Flows: []*entities.SequenceFlow{
				{ID: "f1", SourceRef: "start", TargetRef: "hold"},
				{ID: "f2", SourceRef: "hold", TargetRef: "end"},
			},
		}
		otherID, err := svc.CreateDefinition(ctx, &other)
		if err != nil {
			t.Fatalf("deploy the other process: %v", err)
		}

		err = svc.MigrateInstances(ctx, v1ID, otherID, nil)
		if err == nil {
			t.Fatal("migrating between two different processes should be refused")
		}
		if !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Fatalf("expected an invalid-argument refusal, got %v", err)
		}
	})
}

// A migration whose every waiting node lands is carried out, and the instance
// keeps running — on the target version's graph.
func TestMigrationAppliesAMoveThatLands(t *testing.T) {
	svc, projectID, ctx := releaseFixture(t)

	v1 := approvalModel(projectID, "v1", "hold")
	v1ID, err := svc.CreateDefinition(ctx, &v1)
	if err != nil {
		t.Fatalf("deploy v1: %v", err)
	}
	instanceID, err := svc.StartProcess(ctx, projectID, "expense-approval", nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	v2 := approvalModel(projectID, "v2", "approve")
	v2ID, err := svc.CreateDefinition(ctx, &v2)
	if err != nil {
		t.Fatalf("deploy v2: %v", err)
	}

	if err := svc.MigrateInstances(ctx, v1ID, v2ID, map[string]string{"hold": "approve"}); err != nil {
		t.Fatalf("a mapping that covers every waiting node should be accepted: %v", err)
	}

	instance, err := svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if instance.Definition.ID != v2ID {
		t.Fatalf("the instance should now be on v2, it is on %s", instance.Definition.ID)
	}

	// The task moved with it, and completing it advances through v2's graph.
	tasks, err := svc.ListTasks(ctx, projectID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	var completed bool
	for _, task := range tasks {
		if task.Instance == nil || task.Instance.ID != instanceID {
			continue
		}
		if task.NodeID() != "approve" {
			t.Fatalf("the task should have been rewritten to v2's node, it is on %q", task.NodeID())
		}
		if err := svc.CompleteTask(ctx, task.ID, "approver", nil); err != nil {
			t.Fatalf("complete the migrated task: %v", err)
		}
		completed = true
	}
	if !completed {
		t.Fatal("the migrated instance had no open task")
	}

	instance, err = svc.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("get instance after completion: %v", err)
	}
	if instance.Status != entities.ProcessCompleted {
		t.Fatalf("the migrated instance should have run to the end, got %q", instance.Status)
	}
}
