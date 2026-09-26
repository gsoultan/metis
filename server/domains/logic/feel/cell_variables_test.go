package feel

import (
	"fmt"
	"testing"
)

// maxCellAllocations bounds what testing one cell may allocate. It is a few
// times what a cell costs; what it rules out is a cost that grows with the
// variables the cell could see rather than with the ones it reads.
const maxCellAllocations = 20

// largeOrder is a process's variables at a realistic size: an order with a few
// thousand lines, beside the one figure a cell compares with.
func largeOrder() map[string]any {
	lines := make([]any, 5000)
	for i := range lines {
		lines[i] = map[string]any{"sku": fmt.Sprintf("SKU-%d", i), "quantity": float64(i)}
	}
	return map[string]any{
		"order":   map[string]any{"lines": lines},
		"minimum": 50.0,
	}
}

// TestACellConvertsOnlyTheVariablesItReads keeps a decision table's cost where
// it belongs.
//
// A cell sees every variable the decision was evaluated with, and a table tests
// every cell of every line. Converting all of them into FEEL values for each
// cell made a table's cost grow with the size of the process's data — an order
// with five thousand lines was converted once per cell, whether or not any cell
// mentioned it.
func TestACellConvertsOnlyTheVariablesItReads(t *testing.T) {
	vars := largeOrder()

	for _, cell := range []string{"> 10", "> minimum", `"GOLD", "SILVER"`} {
		allocations := testing.AllocsPerRun(20, func() {
			if _, err := EvaluateUnaryTests(cell, 70.0, vars, nil); err != nil {
				t.Fatalf("cell %q: %v", cell, err)
			}
		})
		if allocations > maxCellAllocations {
			t.Errorf("cell %q allocated %.0f times beside an order it never reads; want at most %d",
				cell, allocations, maxCellAllocations)
		}
	}
}

// TestAVariableReadByACellIsTheVariable checks the conversion that is left:
// what a cell does read, it reads whole — a context through a path, and the
// same name twice in one cell.
func TestAVariableReadByACellIsTheVariable(t *testing.T) {
	vars := map[string]any{
		"limits":  map[string]any{"floor": 10.0, "ceiling": 100.0},
		"minimum": 50.0,
	}
	tests := []struct {
		cell  string
		input any
		want  bool
	}{
		{"[limits.floor..limits.ceiling]", 70.0, true},
		{"[limits.floor..limits.ceiling]", 170.0, false},
		{"> minimum, = minimum", 50.0, true},
		{"> minimum, = minimum", 40.0, false},
	}
	for _, tc := range tests {
		got, err := EvaluateUnaryTests(tc.cell, tc.input, vars, nil)
		if err != nil {
			t.Fatalf("cell %q: %v", tc.cell, err)
		}
		if got != tc.want {
			t.Errorf("cell %q against %v = %v, want %v", tc.cell, tc.input, got, tc.want)
		}
	}
}

// TestInputIsTheCellsOwnValue: `_input` names the cell's own column even when
// the variables hold one of that name, lone or after an operator.
func TestInputIsTheCellsOwnValue(t *testing.T) {
	vars := map[string]any{InputName: 1000.0}
	tests := []struct {
		cell string
		want bool
	}{
		{"_input", true},
		{"= _input", true},
		{"< _input", false},
	}
	for _, tc := range tests {
		got, err := EvaluateUnaryTests(tc.cell, 70.0, vars, nil)
		if err != nil {
			t.Fatalf("cell %q: %v", tc.cell, err)
		}
		if got != tc.want {
			t.Errorf("cell %q against 70 beside a variable _input of 1000 = %v, want %v", tc.cell, got, tc.want)
		}
	}
}

func BenchmarkACellBesideALargeOrder(b *testing.B) {
	vars := largeOrder()
	for b.Loop() {
		if _, err := EvaluateUnaryTests("> minimum", 70.0, vars, nil); err != nil {
			b.Fatal(err)
		}
	}
}
