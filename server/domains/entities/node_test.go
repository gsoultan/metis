package entities_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// A property stored as text may arrive as text (a hand-written definition, an
// imported file) or as the structure the designer saves. Both have to come out
// as the same text; a string-only read turned the designer's form into "".
func TestPropertyTextKeepsTextAndEncodesStructure(t *testing.T) {
	properties := map[string]any{
		"written":  `[{"id":"amount"}]`,
		"designed": []any{map[string]any{"id": "amount"}},
		"nothing":  nil,
	}
	for key, want := range map[string]string{
		"written":  `[{"id":"amount"}]`,
		"designed": `[{"id":"amount"}]`,
		"nothing":  "",
		"absent":   "",
	} {
		if got := entities.PropertyText(properties, key); got != want {
			t.Errorf("%s: got %q, want %q", key, got, want)
		}
	}
	if got := entities.PropertyText(nil, "form_definition"); got != "" {
		t.Errorf("a node with no properties has no text: got %q", got)
	}
}

// BPMN's cancelRemainingInstances defaults to true, so only a sub-process that
// says false keeps the steps still running inside it — as a real boolean, or
// as the text a JSON or XML round trip can leave.
func TestAnAdHocSubProcessCancelsWhatRemainsUnlessToldNotTo(t *testing.T) {
	for _, tc := range []struct {
		name       string
		properties map[string]any
		want       bool
	}{
		{"no properties at all", nil, true},
		{"the property is absent", map[string]any{"other": 1}, true},
		{"explicitly true", map[string]any{entities.CancelRemainingInstancesProperty: true}, true},
		{"explicitly false", map[string]any{entities.CancelRemainingInstancesProperty: false}, false},
		{"false as text", map[string]any{entities.CancelRemainingInstancesProperty: " False "}, false},
		{"anything else as text", map[string]any{entities.CancelRemainingInstancesProperty: "no"}, true},
		{"not a boolean", map[string]any{entities.CancelRemainingInstancesProperty: 0}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := &entities.Node{Properties: tc.properties}
			if got := node.CancelsRemainingInstances(); got != tc.want {
				t.Fatalf("CancelsRemainingInstances() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The runs of a repeating step are counted strictly only when the step is
// work for a person — a user task or a manual task. Nothing else that
// repeats, and nothing that runs once, is.
func TestOnlyARepeatingTaskForAPersonIsARepeatingApproval(t *testing.T) {
	for _, tc := range []struct {
		name string
		node *entities.Node
		want bool
	}{
		{"a parallel user task", &entities.Node{Type: entities.UserTask, MultiInstanceType: "parallel"}, true},
		{"a sequential user task", &entities.Node{Type: entities.UserTask, MultiInstanceType: "sequential"}, true},
		{"a parallel manual task", &entities.Node{Type: entities.ManualTask, MultiInstanceType: "parallel"}, true},
		{"a user task that runs once", &entities.Node{Type: entities.UserTask}, false},
		{"a user task told not to repeat", &entities.Node{Type: entities.UserTask, MultiInstanceType: "none"}, false},
		{"a repeating sub-process", &entities.Node{Type: entities.SubProcess, MultiInstanceType: "parallel"}, false},
		{"a repeating ad-hoc sub-process", &entities.Node{Type: entities.SubProcess, IsAdHoc: true, MultiInstanceType: "parallel"}, false},
		{"a repeating service task", &entities.Node{Type: entities.ServiceTask, MultiInstanceType: "parallel"}, false},
		{"a repeating external task", &entities.Node{Type: entities.ServiceTask, ExternalTopic: "check", MultiInstanceType: "sequential"}, false},
		{"a repeating call activity", &entities.Node{Type: entities.CallActivity, MultiInstanceType: "parallel"}, false},
		{"a repeating script", &entities.Node{Type: entities.ScriptTask, MultiInstanceType: "parallel"}, false},
		{"no step at all", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.node.IsRepeatingApproval(); got != tc.want {
				t.Fatalf("IsRepeatingApproval() = %v, want %v", got, tc.want)
			}
		})
	}

	if (&entities.Node{Type: entities.SubProcess, MultiInstanceType: "parallel"}).Repeats() != true {
		t.Error("a sub-process run once per item does not read as repeating")
	}
	if (*entities.Node)(nil).Repeats() {
		t.Error("no step at all reads as repeating")
	}
}
