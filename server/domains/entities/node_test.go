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
