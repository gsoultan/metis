package bpmn_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// What happens when a step goes wrong.
//
// An error boundary event is the answer to "the payment provider was down" —
// the process takes a different path instead of stopping. The engine has one,
// the designer can attach one, and nothing ran one until now.

// failingEndpoint answers every request with 500.
func failingEndpoint(t *testing.T) string {
	t.Helper()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"the provider is down"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(api.Close)
	return api.URL
}

// A boundary event with no error code catches anything, which is how you say
// "if this goes wrong at all, do that instead".
func TestErrorBoundary_TakesTheRecoveryPathWhenTheTaskFails(t *testing.T) {
	t.Setenv("GOBPM_HTTP_ALLOW_PRIVATE_NETWORKS", "true")

	h := newServiceTaskHarness(t)
	instance := h.runDefinition(t, &entities.ProcessDefinition{
		Key:  "boundary-catch-all",
		Name: "Charge the card, or ask someone",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "charge", Type: entities.ServiceTask, Name: "Charge the card", Properties: map[string]any{
				"http_url":    failingEndpoint(t),
				"http_method": "POST",
			}},
			{ID: "failed", Type: entities.BoundaryEvent, Name: "If the charge fails",
				AttachedToRef: "charge", CancelActivity: true},
			{ID: "review", Type: entities.UserTask, Name: "Take payment another way", Assignee: "carol"},
			{ID: "end", Type: entities.EndEvent, Name: "Done"},
			{ID: "end-failed", Type: entities.EndEvent, Name: "Handled by hand"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "charge"},
			{ID: "f2", SourceRef: "charge", TargetRef: "end"},
			{ID: "f3", SourceRef: "failed", TargetRef: "review"},
			{ID: "f4", SourceRef: "review", TargetRef: "end-failed"},
		},
	}, map[string]any{"amount": 40})

	tasks := h.tasksFor(t, instance.ID)
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want the one the boundary routes to — the failure did not take the recovery path: %v",
			len(tasks), taskNames(tasks))
	}
	if tasks[0].Name != "Take payment another way" {
		t.Errorf("the process created %q, not the recovery task", tasks[0].Name)
	}
}

// A boundary event with a code catches only that code, so an unrelated failure
// is still a failure rather than quietly taking someone else's recovery path.
func TestErrorBoundary_LeavesAnErrorItsCodeDoesNotMatch(t *testing.T) {
	t.Setenv("GOBPM_HTTP_ALLOW_PRIVATE_NETWORKS", "true")

	h := newServiceTaskHarness(t)
	instance := h.runDefinition(t, &entities.ProcessDefinition{
		Key:  "boundary-specific-code",
		Name: "Only catch a card decline",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "charge", Type: entities.ServiceTask, Name: "Charge the card", Properties: map[string]any{
				"http_url":    failingEndpoint(t),
				"http_method": "POST",
			}},
			{ID: "declined", Type: entities.BoundaryEvent, Name: "If the card is declined",
				AttachedToRef: "charge", CancelActivity: true, ErrorCode: "CARD_DECLINED"},
			{ID: "retry", Type: entities.UserTask, Name: "Ask for another card", Assignee: "carol"},
			{ID: "end", Type: entities.EndEvent, Name: "Done"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "charge"},
			{ID: "f2", SourceRef: "charge", TargetRef: "end"},
			{ID: "f3", SourceRef: "declined", TargetRef: "retry"},
		},
	}, map[string]any{})

	if tasks := h.tasksFor(t, instance.ID); len(tasks) != 0 {
		t.Errorf("a boundary for CARD_DECLINED caught an unrelated failure and created %v", taskNames(tasks))
	}
	if instance.Status == entities.ProcessCompleted {
		t.Error("the process completed even though the charge failed and nothing caught it")
	}
	if job := h.lastJob(t, instance.ID); job.LastError == "" {
		t.Error("nothing was recorded against the failed job")
	}
}

// The task the boundary interrupts must not also carry on. Both paths running
// is worse than either: the card gets charged and someone is also asked to
// take payment another way.
func TestErrorBoundary_InterruptingBoundaryStopsTheTaskItIsAttachedTo(t *testing.T) {
	t.Setenv("GOBPM_HTTP_ALLOW_PRIVATE_NETWORKS", "true")

	h := newServiceTaskHarness(t)
	instance := h.runDefinition(t, &entities.ProcessDefinition{
		Key:  "boundary-interrupts",
		Name: "One path or the other",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "charge", Type: entities.ServiceTask, Name: "Charge the card", Properties: map[string]any{
				"http_url":    failingEndpoint(t),
				"http_method": "POST",
			}},
			{ID: "failed", Type: entities.BoundaryEvent, Name: "If the charge fails",
				AttachedToRef: "charge", CancelActivity: true},
			{ID: "review", Type: entities.UserTask, Name: "Take payment another way", Assignee: "carol"},
			{ID: "receipt", Type: entities.UserTask, Name: "Send the receipt", Assignee: "carol"},
			{ID: "end", Type: entities.EndEvent, Name: "Done"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "charge"},
			{ID: "f2", SourceRef: "charge", TargetRef: "receipt"},
			{ID: "f3", SourceRef: "failed", TargetRef: "review"},
			{ID: "f4", SourceRef: "review", TargetRef: "end"},
			{ID: "f5", SourceRef: "receipt", TargetRef: "end"},
		},
	}, map[string]any{})

	for _, task := range h.tasksFor(t, instance.ID) {
		if task.Name == "Send the receipt" {
			t.Error("the charge failed and a receipt was still sent — the boundary did not interrupt the task")
		}
	}
}

// A boundary event that catches something has to leave the process able to
// finish; the recovery path is a path, not a dead end.
func TestErrorBoundary_TheRecoveryPathCanBeCompleted(t *testing.T) {
	t.Setenv("GOBPM_HTTP_ALLOW_PRIVATE_NETWORKS", "true")

	h := newServiceTaskHarness(t)
	instance := h.runDefinition(t, &entities.ProcessDefinition{
		Key:  "boundary-completes",
		Name: "Recover and finish",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Name: "Start"},
			{ID: "charge", Type: entities.ServiceTask, Name: "Charge the card", Properties: map[string]any{
				"http_url":    failingEndpoint(t),
				"http_method": "POST",
			}},
			{ID: "failed", Type: entities.BoundaryEvent, AttachedToRef: "charge", CancelActivity: true},
			{ID: "review", Type: entities.UserTask, Name: "Take payment another way", Assignee: "carol"},
			{ID: "end", Type: entities.EndEvent, Name: "Done"},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "charge"},
			{ID: "f2", SourceRef: "charge", TargetRef: "end"},
			{ID: "f3", SourceRef: "failed", TargetRef: "review"},
			{ID: "f4", SourceRef: "review", TargetRef: "end"},
		},
	}, map[string]any{})

	tasks := h.tasksFor(t, instance.ID)
	if len(tasks) != 1 {
		t.Fatalf("expected the recovery task, got %v", taskNames(tasks))
	}

	if err := h.taskSvc.CompleteTask(t.Context(), tasks[0].ID, "carol", map[string]any{"paidBy": "bank transfer"}); err != nil {
		t.Fatalf("complete the recovery task: %v", err)
	}

	reloaded, err := h.engine.GetInstance(t.Context(), instance.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Status != entities.ProcessCompleted {
		t.Errorf("after the recovery task was done the instance is %q, want completed", reloaded.Status)
	}
	if reloaded.Variables["paidBy"] != "bank transfer" {
		t.Errorf("what the recovery task recorded did not reach the instance: %v", reloaded.Variables)
	}
}

func taskNames(tasks []entities.Task) []string {
	out := make([]string, len(tasks))
	for i, t := range tasks {
		out[i] = t.Name
	}
	return out
}

// The error code can be written in two places, and both paths must read both.
//
// ErrorCode is a field on the node; "error_code" is a property. The in-process
// path read the property and the job worker read the field, so a definition
// setting only one worked on one path and silently not on the other — and the
// designer writes the field.
func TestErrorBoundary_MatchesWhicheverWayTheCodeWasWritten(t *testing.T) {
	for _, tc := range []struct {
		name string
		node *entities.Node
	}{
		{
			name: "written as a field, which is what the designer saves",
			node: &entities.Node{ID: "recover", Type: entities.BoundaryEvent, AttachedToRef: "charge",
				ErrorCode: "charge-failed"},
		},
		{
			name: "written as a property, which older definitions carry",
			node: &entities.Node{ID: "recover", Type: entities.BoundaryEvent, AttachedToRef: "charge",
				Properties: map[string]any{"error_code": "charge-failed"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.node.ErrorCodeValue(); got != "charge-failed" {
				t.Errorf("the code was not found: got %q", got)
			}
			if !tc.node.IsErrorBoundary() {
				t.Error("a node carrying an error code was not recognised as an error boundary")
			}
		})
	}
}

// A boundary event configured as something else must not swallow failures.
func TestErrorBoundary_LeavesFailuresToBoundariesThatAreNotErrorEvents(t *testing.T) {
	for _, tc := range []struct {
		name string
		node *entities.Node
	}{
		{name: "a timer boundary", node: &entities.Node{ID: "deadline", Type: entities.BoundaryEvent,
			Properties: map[string]any{"timer_duration": "PT2H"}}},
		{name: "a message boundary", node: &entities.Node{ID: "cancelled", Type: entities.BoundaryEvent,
			Properties: map[string]any{"message_name": "OrderCancelled"}}},
		{name: "an escalation boundary", node: &entities.Node{ID: "raise", Type: entities.BoundaryEvent,
			Properties: map[string]any{"escalation_code": "over-limit"}}},
		{name: "a compensation boundary", node: &entities.Node{ID: "undo", Type: entities.BoundaryEvent,
			Properties: map[string]any{"event_type": "compensation"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.node.IsErrorBoundary() {
				t.Error("a boundary event configured as another kind of event would catch failures meant for an error boundary")
			}
		})
	}
}

// A bare boundary event is still the catch-all it has always been.
func TestErrorBoundary_ABareBoundaryStillCatchesAnything(t *testing.T) {
	bare := &entities.Node{ID: "recover", Type: entities.BoundaryEvent, AttachedToRef: "charge"}
	if !bare.IsErrorBoundary() {
		t.Error("a boundary event with nothing on it stopped being a catch-all")
	}
	if bare.ErrorCodeValue() != "" {
		t.Error("a bare boundary event reported a code it does not have")
	}
}
