package clamp

import (
	"math"
	"testing"
)

// The property that matters: too-large comes out large, never negative.
// Wrapping turns "very big" into "before zero", which is not a smaller
// inaccuracy than clamping — it is a different statement.
func TestInt32DoesNotWrapIntoNegative(t *testing.T) {
	// The coordinate a crafted BPMN file would carry. A plain int32() of this
	// is -1294967296, and the node lands on the far side of the canvas.
	if got := Int32(3_000_000_000); got != math.MaxInt32 {
		t.Errorf("Int32(3e9) = %d, want %d", got, math.MaxInt32)
	}
	if got := Int32(-3_000_000_000); got != math.MinInt32 {
		t.Errorf("Int32(-3e9) = %d, want %d", got, math.MinInt32)
	}
}

func TestInt32LeavesRepresentableValuesAlone(t *testing.T) {
	for _, v := range []int{0, 1, -1, 4096, -4096, math.MaxInt32, math.MinInt32} {
		if got := Int32(v); int(got) != v {
			t.Errorf("Int32(%d) = %d, want it unchanged", v, got)
		}
	}
}

// Clamping is only defensible if it keeps comparisons meaningful; wrapping
// does not. Two values that were ordered must stay ordered.
func TestInt32PreservesOrder(t *testing.T) {
	cases := [][2]int{
		{4096, 8192},
		{math.MaxInt32 - 1, math.MaxInt32},
		{math.MaxInt32, 3_000_000_000},
		{-3_000_000_000, 0},
	}
	for _, c := range cases {
		lo, hi := Int32(c[0]), Int32(c[1])
		if lo > hi {
			t.Errorf("Int32 inverted the order of %d and %d: got %d > %d", c[0], c[1], lo, hi)
		}
	}
}

func TestInt32FromInt64(t *testing.T) {
	if got := Int32FromInt64(math.MaxInt64); got != math.MaxInt32 {
		t.Errorf("Int32FromInt64(MaxInt64) = %d, want %d", got, math.MaxInt32)
	}
	if got := Int32FromInt64(math.MinInt64); got != math.MinInt32 {
		t.Errorf("Int32FromInt64(MinInt64) = %d, want %d", got, math.MinInt32)
	}
	if got := Int32FromInt64(1234); got != 1234 {
		t.Errorf("Int32FromInt64(1234) = %d, want 1234", got)
	}
}

// The unsigned half above MaxInt64 has no int64 representation, so a plain
// conversion produces a negative number — which for a counter reads as
// "before the beginning".
func TestInt64FromUint64DoesNotGoNegative(t *testing.T) {
	if got := Int64FromUint64(math.MaxUint64); got != math.MaxInt64 {
		t.Errorf("Int64FromUint64(MaxUint64) = %d, want %d", got, int64(math.MaxInt64))
	}
	if got := Int64FromUint64(uint64(math.MaxInt64) + 1); got != math.MaxInt64 {
		t.Errorf("Int64FromUint64(MaxInt64+1) = %d, want %d", got, int64(math.MaxInt64))
	}
	if got := Int64FromUint64(42); got != 42 {
		t.Errorf("Int64FromUint64(42) = %d, want 42", got)
	}
}

func TestIntFromUint64DoesNotGoNegative(t *testing.T) {
	if got := IntFromUint64(math.MaxUint64); got != math.MaxInt {
		t.Errorf("IntFromUint64(MaxUint64) = %d, want %d", got, math.MaxInt)
	}
	if got := IntFromUint64(7); got != 7 {
		t.Errorf("IntFromUint64(7) = %d, want 7", got)
	}
}
