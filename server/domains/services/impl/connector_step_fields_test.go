package impl

import (
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

func TestWithStepFields(t *testing.T) {
	variables := map[string]any{"po": "PO-17", "amount": 4000, "text": "from a variable"}
	node := entities.Node{ID: "notify", Properties: map[string]any{
		"connector_field_text":    "Approve {{po}} for {{amount}} EUR",
		"connector_field_subject": "  ",
		"connector_statement":     "not a field",
	}}

	got, err := withStepFields(node, variables, variables)
	if err != nil {
		t.Fatalf("withStepFields: %v", err)
	}
	if got["text"] != "Approve PO-17 for 4000 EUR" {
		t.Errorf("text = %v, want the filled-in field over the variable", got["text"])
	}
	if _, sent := got["subject"]; sent {
		t.Error("a blank field was sent")
	}
	if _, sent := got["statement"]; sent {
		t.Error("a property that is not a step field was sent")
	}
	if variables["text"] != "from a variable" {
		t.Error("the instance's own variables were changed")
	}
}

func TestAStepFieldThatCannotBeFilledInFailsTheStep(t *testing.T) {
	node := entities.Node{ID: "notify", Properties: map[string]any{"connector_field_text": "Approve {{po"}}
	_, err := withStepFields(node, map[string]any{}, map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "text") {
		t.Errorf("error = %v, want one naming the text field", err)
	}
}

func TestAStepWithNoFieldsSendsItsInputsAsTheyAre(t *testing.T) {
	inputs := map[string]any{"text": "hi"}
	got, err := withStepFields(entities.Node{Properties: map[string]any{}}, inputs, inputs)
	if err != nil || got["text"] != "hi" {
		t.Errorf("got %v, %v; want the inputs unchanged", got, err)
	}
}
