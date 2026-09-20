// Package clamp narrows integers without wrapping.
//
// The protobuf surface is int32 and the Go side is int, so every transport
// adapter converts between them. A plain conversion wraps silently: a diagram
// coordinate of 3,000,000,000 becomes -1,294,967,296, and a node jumps to the
// other side of the canvas with nothing logged.
//
// Coordinates arrive in imported BPMN, which AGENTS.md §0 names as untrusted
// input, so this is reachable by a crafted file rather than only by an absurd
// one. Counts come from the database and are merely implausible rather than
// hostile, but they go through the same boundary.
//
// Clamping is the right answer at this boundary for the reason wrapping is the
// wrong one: it preserves order. A value too large to represent comes out as
// the largest representable value, which is still "very big"; wrapping comes
// out as "negative", which is a different and wrong statement. Neither is the
// true number, and only one of them keeps comparisons meaningful.
//
// It is deliberately not an error. These are presentation fields on a wire
// type; refusing to serialise a whole process because one shape is absurdly
// wide would turn a cosmetic problem into an outage.
package clamp

import "math"

// Int32 narrows an int to an int32, clamping to the int32 range.
func Int32(v int) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}

// Int32FromInt64 narrows an int64 to an int32, clamping to the int32 range.
func Int32FromInt64(v int64) int32 {
	if v > math.MaxInt32 {
		return math.MaxInt32
	}
	if v < math.MinInt32 {
		return math.MinInt32
	}
	return int32(v)
}

// Int64FromUint64 narrows a uint64 to an int64, clamping at MaxInt64.
//
// The unsigned half above MaxInt64 has no int64 representation at all, so a
// plain conversion there produces a negative number — which for the counters
// and lock identifiers this is used on would read as "before the beginning".
func Int64FromUint64(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

// IntFromUint64 narrows a uint64 to an int, clamping at MaxInt.
func IntFromUint64(v uint64) int {
	if v > math.MaxInt {
		return math.MaxInt
	}
	return int(v)
}
