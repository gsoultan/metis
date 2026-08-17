package impl

import (
	"testing"
)

// TestResolveMapping covers the third surface of Phase 2.2. A mapping source
// used to be a variable name and nothing else, so computing a value meant
// adding a script task beside the decision purely to do arithmetic.
func TestResolveMapping(t *testing.T) {
	source := map[string]any{
		"price":    10.0,
		"quantity": 3.0,
		"customer": map[string]any{"country": "GB"},
		"items": []any{
			map[string]any{"price": 10.0},
			map[string]any{"price": 5.0},
		},
	}

	tests := []struct {
		name    string
		mapping map[string]any
		want    map[string]any
	}{
		{
			name:    "a bare name is still a plain rename",
			mapping: map[string]any{"unitPrice": "price"},
			want:    map[string]any{"unitPrice": 10.0},
		},
		{
			name:    "arithmetic, which previously needed a script task",
			mapping: map[string]any{"total": "price * quantity"},
			want:    map[string]any{"total": 30.0},
		},
		{
			name:    "a property path",
			mapping: map[string]any{"country": "customer.country"},
			want:    map[string]any{"country": "GB"},
		},
		{
			name:    "an aggregate over a list",
			mapping: map[string]any{"basket": "sum(items.price)"},
			want:    map[string]any{"basket": 15.0},
		},
		{
			name:    "a non-string source is a constant",
			mapping: map[string]any{"flag": true},
			want:    map[string]any{"flag": true},
		},
		{
			// The name-only version left an absent variable unset rather than
			// writing null, and that behaviour is preserved.
			name:    "an absent source leaves the target unset",
			mapping: map[string]any{"missing": "nothingHere"},
			want:    map[string]any{},
		},
		{
			name:    "a broken expression leaves the target unset rather than failing the node",
			mapping: map[string]any{"broken": "price *"},
			want:    map[string]any{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveMapping(tc.mapping, source)

			if len(got) != len(tc.want) {
				t.Fatalf("resolveMapping = %v, want %v", got, tc.want)
			}
			for key, want := range tc.want {
				if got[key] != want {
					t.Errorf("%s = %v, want %v", key, got[key], want)
				}
			}
		})
	}
}

// TestResolveMappingPrefersAVariableOverAnExpression keeps a variable whose
// name collides with FEEL syntax working: the plain lookup is tried first.
func TestResolveMappingPrefersAVariableOverAnExpression(t *testing.T) {
	source := map[string]any{"true": "not a boolean"}
	got := resolveMapping(map[string]any{"x": "true"}, source)
	if got["x"] != "not a boolean" {
		t.Errorf(`x = %v, want the variable named "true" rather than the FEEL literal`, got["x"])
	}
}
