package bpmn_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
)

// Undoing work that was already done.
//
// Compensation is the rollback half of a saga: "book the flight" is undone by
// "cancel the flight". The engine has one, and the only thing that ever ran it
// was a single happy-path assertion. What it never checked is the property that
// makes compensation safe to retry — that an activity already rolled back is not
// rolled back a second time.

// countTasksAt reports how many open tasks the instance has at nodeID. A second
// compensation shows up here as a second task.
func (h engineHarness) countTasksAt(ctx context.Context, t *testing.T, instanceID uuid.UUID, nodeID string) int {
	t.Helper()

	tasks, err := h.svc.ListTasks(ctx, h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	count := 0
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == nodeID {
			count++
		}
	}
	return count
}

func compensationDefinition(projID uuid.UUID, key string) entities.ProcessDefinition {
	return entities.ProcessDefinition{
		Project: &entities.Project{ID: projID},
		Key:     key,
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "book-flight", Type: entities.UserTask, Name: "Book the flight"},
			// What the designer writes for a compensation boundary event:
			// definitionMapper maps eventType -> event_type.
			{ID: "flight-comp", Type: entities.BoundaryEvent, AttachedToRef: "book-flight", Properties: map[string]any{
				"event_type": "compensation",
			}},
			{ID: "cancel-flight", Type: entities.UserTask, Name: "Cancel the flight"},
			{ID: "comp-throw", Type: entities.CompensationThrowEvent},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "book-flight"},
			{ID: "f2", SourceRef: "book-flight", TargetRef: "comp-throw"},
			{ID: "f3", SourceRef: "comp-throw", TargetRef: "end"},
			{ID: "f4", SourceRef: "flight-comp", TargetRef: "cancel-flight"},
		},
	}
}

// A compensation triggered a second time must not undo the same activity twice.
//
// The instance is reloaded from the database in between, which is what happens
// on a retry, a crash recovery, or a second compensation throw. That reload is
// the whole point: the instance adapter rebuilds CompensatedNodes as fresh
// pointers, so a dedupe check based on pointer identity stops working precisely
// when it is needed.
func TestCompensationDoesNotRunTwiceForTheSameActivity(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Compensation Project")

	def := compensationDefinition(h.projID, "booking")
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "booking", map[string]any{"pnr": "AB123"})
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	// Do the work, which then flows into the compensation throw.
	tasks, err := h.svc.ListTasks(ctx, h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	booked := false
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == "book-flight" {
			if err := h.svc.CompleteTask(ctx, task.ID, "carol", nil); err != nil {
				t.Fatalf("complete the booking task: %v", err)
			}
			booked = true
		}
	}
	if !booked {
		t.Fatal("the process never offered the booking task")
	}

	if got := h.countTasksAt(ctx, t, instanceID, "cancel-flight"); got != 1 {
		t.Fatalf("expected the flight to be cancelled exactly once, got %d cancellations", got)
	}

	// Reload the instance the way a retry or a second throw would.
	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	reloadedDef, err := h.engine.GetProcessDefinition(ctx, instance.Definition.ID)
	if err != nil {
		t.Fatalf("reload definition: %v", err)
	}
	throwNode := reloadedDef.FindNode("comp-throw")
	if throwNode == nil {
		t.Fatal("the compensation throw event is missing from the reloaded definition")
	}

	if err := h.engine.TriggerCompensation(ctx, &instance, reloadedDef, *throwNode, ""); err != nil {
		t.Fatalf("second compensation: %v", err)
	}

	if got := h.countTasksAt(ctx, t, instanceID, "cancel-flight"); got != 1 {
		t.Errorf("the flight was cancelled %d times — an activity already compensated was rolled back again", got)
	}
}

// Compensating one named activity must not touch the others.
func TestCompensationOfANamedActivityLeavesOthersAlone(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Named Compensation Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "booking-pair",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "book-flight", Type: entities.UserTask, Name: "Book the flight"},
			{ID: "flight-comp", Type: entities.BoundaryEvent, AttachedToRef: "book-flight", Properties: map[string]any{
				"event_type": "compensation",
			}},
			{ID: "cancel-flight", Type: entities.UserTask, Name: "Cancel the flight"},
			{ID: "book-hotel", Type: entities.UserTask, Name: "Book the hotel"},
			{ID: "hotel-comp", Type: entities.BoundaryEvent, AttachedToRef: "book-hotel", Properties: map[string]any{
				"event_type": "compensation",
			}},
			{ID: "cancel-hotel", Type: entities.UserTask, Name: "Cancel the hotel"},
			// Undo only the flight.
			{ID: "comp-throw", Type: entities.CompensationThrowEvent, Properties: map[string]any{
				"activity_ref": "book-flight",
			}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "book-flight"},
			{ID: "f2", SourceRef: "book-flight", TargetRef: "book-hotel"},
			{ID: "f3", SourceRef: "book-hotel", TargetRef: "comp-throw"},
			{ID: "f4", SourceRef: "comp-throw", TargetRef: "end"},
			{ID: "f5", SourceRef: "flight-comp", TargetRef: "cancel-flight"},
			{ID: "f6", SourceRef: "hotel-comp", TargetRef: "cancel-hotel"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "booking-pair", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	// Work through both bookings; the second completion reaches the throw.
	for _, nodeID := range []string{"book-flight", "book-hotel"} {
		tasks, err := h.svc.ListTasks(ctx, h.projID)
		if err != nil {
			t.Fatalf("list tasks: %v", err)
		}
		done := false
		for _, task := range tasks {
			if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == nodeID {
				if err := h.svc.CompleteTask(ctx, task.ID, "carol", nil); err != nil {
					t.Fatalf("complete %s: %v", nodeID, err)
				}
				done = true
			}
		}
		if !done {
			t.Fatalf("the process never offered %s", nodeID)
		}
	}

	if got := h.countTasksAt(ctx, t, instanceID, "cancel-flight"); got != 1 {
		t.Errorf("expected the named activity to be compensated once, got %d", got)
	}
	if got := h.countTasksAt(ctx, t, instanceID, "cancel-hotel"); got != 0 {
		t.Errorf("the hotel was compensated %d times even though only the flight was named", got)
	}
}

// A compensation whose handler fails must not be recorded as done.
//
// The activity is marked compensated so it is not rolled back twice, and that
// mark is what IsCompensated checks. Writing it before the handler has actually
// run means a failed rollback is remembered as a successful one, and the guard
// then blocks every retry — the business reversal never happens and nothing
// says so.
func TestFailedCompensationIsNotRecordedAsDone(t *testing.T) {
	ctx := t.Context()
	h := newEngineHarness(t, "Failed Compensation Project")

	def := entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "booking-failing-comp",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "book-flight", Type: entities.UserTask, Name: "Book the flight"},
			{ID: "flight-comp", Type: entities.BoundaryEvent, AttachedToRef: "book-flight", Properties: map[string]any{
				"event_type": "compensation",
			}},
			// The rollback itself fails.
			{ID: "cancel-flight", Type: entities.ScriptTask, Name: "Cancel the flight",
				Script: `throw new Error("the airline API rejected the cancellation");`},
			{ID: "comp-throw", Type: entities.CompensationThrowEvent},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "book-flight"},
			{ID: "f2", SourceRef: "book-flight", TargetRef: "comp-throw"},
			{ID: "f3", SourceRef: "comp-throw", TargetRef: "end"},
			{ID: "f4", SourceRef: "flight-comp", TargetRef: "cancel-flight"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, &def); err != nil {
		t.Fatalf("create definition: %v", err)
	}

	instanceID, err := h.svc.StartProcess(ctx, h.projID, "booking-failing-comp", nil)
	if err != nil {
		t.Fatalf("start process: %v", err)
	}

	tasks, err := h.svc.ListTasks(ctx, h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, task := range tasks {
		if task.Instance != nil && task.Instance.ID == instanceID && task.NodeID() == "book-flight" {
			// The failing rollback may surface here; either way is fine.
			_ = h.svc.CompleteTask(ctx, task.ID, "carol", nil)
		}
	}

	instance, err := h.engine.GetInstance(ctx, instanceID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	for _, n := range instance.CompensatedNodes {
		if n != nil && n.ID == "book-flight" {
			t.Error("the flight was recorded as compensated even though the cancellation failed; the rollback can never be retried")
		}
	}
}
