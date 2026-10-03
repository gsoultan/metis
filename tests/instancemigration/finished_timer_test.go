package instancemigration

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// A timer that has already fired.
//
// A timer is a row that outlives its firing: it cannot be deleted, so it stays
// behind marked completed, one for every occurrence of a repeating one. The
// landing check counted every such row as work parked on its step. An instance
// that had long since passed a wait the new version dropped was refused for
// "work parked" there, and nothing could be done about it but map a step
// nobody was on.

// TestATimerThatAlreadyFiredDoesNotStopAMigration.
//
// Root cause: the landing check counted an instance's job rows whatever their
// status, finished ones included.
func TestATimerThatAlreadyFiredDoesNotStopAMigration(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, coolingOff(f.project, "wait", "work", true), coolingOff(f.project, "wait", "work", false))
	instance := f.onlyInstance(t)
	// The hour passes: the wait is over and the instance is at the step after
	// it, which the new version keeps.
	if err := f.db.WithContext(f.ctx).Exec(
		`UPDATE jobs SET next_run_at = now() - interval '1 minute' WHERE instance_id = ?`, instance.ID).Error; err != nil {
		t.Fatalf("bring the timer due: %v", err)
	}
	if err := f.svc.ProcessPendingJobs(f.ctx); err != nil {
		t.Fatalf("process pending jobs: %v", err)
	}
	f.assertWaitingAt(t, v1, "work")
	if rows := f.jobRows(t, instance.ID); len(rows) != 1 || !strings.HasSuffix(rows[0], "status=completed") {
		t.Fatalf("the timer should have fired and be finished: %v", rows)
	}

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !plan.Applicable() {
		t.Fatalf("the migration was refused for a timer that had already fired: %v", plan.Refusals)
	}
	for _, move := range plan.Moves {
		if move.From == "wait" {
			t.Errorf("the plan shows work on the wait (%+v); nothing is waiting there", move)
		}
	}

	result, err := f.svc.ApplyInstanceMigration(f.ctx, v1, v2, nil, servicecontracts.WithActor("dita"))
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Changed != 1 || len(result.PassedOver) != 0 {
		t.Errorf("the run says it acted on %d and passed over %d (%+v), want one and none", result.Changed, len(result.PassedOver), result.PassedOver)
	}
	f.assertWaitingAt(t, v2, "work")
	f.assertNothingIsStranded(t)
	f.runsToItsEnd(t)
}

// deadlined is an approval with a three-day deadline on it that leads to an
// escalation, and without the deadline when withDeadline is false.
func deadlined(projectID uuid.UUID, withDeadline bool) *entities.ProcessDefinition {
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "deadlined-approval",
		Name:    "Deadlined approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"d1"}},
			{ID: "approve", Name: "Approve", Type: entities.UserTask, Assignee: "ada", Incoming: []string{"d1"}, Outgoing: []string{"d2"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"d2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "d1", SourceRef: "start", TargetRef: "approve"},
			{ID: "d2", SourceRef: "approve", TargetRef: "end"},
		},
	}
	if !withDeadline {
		return def
	}
	def.Nodes = append(def.Nodes,
		&entities.Node{
			ID: "deadline", Name: "Three days passed", Type: entities.BoundaryEvent, AttachedToRef: "approve",
			Properties: map[string]any{"event_type": "timer", "timer_duration": "P3D"}, Outgoing: []string{"d3"},
		},
		&entities.Node{ID: "escalate", Name: "Escalate", Type: entities.UserTask, Assignee: "bo", Incoming: []string{"d3"}, Outgoing: []string{"d4"}},
		&entities.Node{ID: "escalated", Type: entities.EndEvent, Incoming: []string{"d4"}})
	def.Flows = append(def.Flows,
		&entities.SequenceFlow{ID: "d3", SourceRef: "deadline", TargetRef: "escalate"},
		&entities.SequenceFlow{ID: "d4", SourceRef: "escalate", TargetRef: "escalated"})
	return def
}

// A timer that has not fired is still work: a deadline running on an approval
// has to have somewhere to go, and a version without it is still refused. The
// change above leaves out only rows that are finished.
func TestATimerStillRunningOnAStepTheNewVersionLacksStillStopsTheMigration(t *testing.T) {
	f := newFixture(t)
	v1, v2 := f.startedOn(t, deadlined(f.project, true), deadlined(f.project, false))
	instance := f.onlyInstance(t)
	if rows := f.jobRows(t, instance.ID); len(rows) != 1 || !strings.Contains(rows[0], "node=deadline ") || !strings.HasSuffix(rows[0], "status=pending") {
		t.Fatalf("the deadline should be running: %v", rows)
	}

	plan, err := f.svc.PlanInstanceMigration(f.ctx, v1, v2, nil)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Applicable() || !strings.Contains(strings.Join(plan.Refusals, "; "), "nowhere to put the work parked on deadline") {
		t.Fatalf("the migration should be refused for the deadline still running: %v", plan.Refusals)
	}
	if err := f.svc.MigrateInstances(f.ctx, v1, v2, nil); err == nil {
		t.Fatal("the apply accepted what the plan refused")
	}
	f.assertWaitingAt(t, v1, "approve")
}
