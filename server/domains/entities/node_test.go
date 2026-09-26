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
