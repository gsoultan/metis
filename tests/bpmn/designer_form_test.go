package bpmn_test

import (
	"encoding/json"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A form built in the designer is saved on the step as a list of fields. The
// task copied it with a string-only read, which answers "" for anything that is
// not a string — so every form built in the designer reached its task empty,
// and the person doing the work got no fields to fill in.
func TestAFormBuiltInTheDesignerReachesItsTask(t *testing.T) {
	h := newEngineHarness(t, "Designer Form Project")
	ctx := h.Ctx()

	// As the designer's deploy payload arrives once decoded: a list of objects.
	form := []any{
		map[string]any{"id": "amount", "label": "Amount", "type": "number", "required": true},
		map[string]any{"id": "reason", "label": "Reason", "type": "textarea"},
	}
	h.deploy(t, &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "expense-claim",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "claim", Type: entities.UserTask, Name: "Claim expenses", Properties: map[string]any{"form_definition": form}},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "claim"},
			{ID: "f2", SourceRef: "claim", TargetRef: "end"},
		},
	})
	if _, err := h.svc.StartProcess(ctx, h.projID, "expense-claim", nil); err != nil {
		t.Fatalf("start: %v", err)
	}

	tasks, err := h.svc.ListTasks(ctx, h.projID)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("expected the claim task: %d tasks, err=%v", len(tasks), err)
	}
	var fields []map[string]any
	if err := json.Unmarshal([]byte(tasks[0].FormDefinition), &fields); err != nil {
		t.Fatalf("the task's form is not the designer's list of fields: %q (%v)", tasks[0].FormDefinition, err)
	}
	if len(fields) != 2 || fields[0]["id"] != "amount" || fields[1]["id"] != "reason" {
		t.Fatalf("the task's form lost fields on the way: %v", fields)
	}
}
