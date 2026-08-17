// Package feel implements the subset of FEEL (Friendly Enough Expression
// Language, DMN 1.4) that this engine needs.
//
// It exists because the thing it replaces was not FEEL. The previous evaluator
// matched strings: it could compare a number against a bound and split a list on
// commas, and that was the whole language. No dates, no durations, no
// arithmetic, no `and`/`or`, no property paths, no built-in functions — and a
// comma inside a range or a string broke it, because splitting came before
// parsing.
//
// The design is a hand-written lexer and Pratt parser producing an AST,
// evaluated over a typed value model. That matters beyond correctness: gateway
// conditions are authored by users and were previously handed to a JavaScript
// runtime, where a single expression could hold a worker for 37 seconds and
// allocate without bound. Nothing here can loop, recurse without limit, or
// allocate on the user's behalf — the language has no constructs for it.
//
// The supported subset is documented in .junie/execution-plan.md §2.1 and
// asserted by TestSubsetIsDocumented. Deliberately absent, and left absent
// rather than faked: boxed contexts, for/return, some/every, user-defined
// functions, external functions.
package feel

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Kind is the type of a FEEL value.
type Kind uint8

const (
	KindNull Kind = iota
	KindNumber
	KindString
	KindBoolean
	KindDate
	KindTime
	KindDateTime
	KindDuration
	KindList
	KindContext
)

func (k Kind) String() string {
	switch k {
	case KindNull:
		return "null"
	case KindNumber:
		return "number"
	case KindString:
		return "string"
	case KindBoolean:
		return "boolean"
	case KindDate:
		return "date"
	case KindTime:
		return "time"
	case KindDateTime:
		return "date and time"
	case KindDuration:
		return "duration"
	case KindList:
		return "list"
	case KindContext:
		return "context"
	}
	return "unknown"
}

// Value is a typed FEEL value.
//
// Numbers are float64 rather than an arbitrary-precision decimal, which the
// FEEL specification nominally calls for. The reason is honesty about where the
// data comes from: process variables arrive as JSON, so a number has already
// been through float64 before any expression sees it. Adding decimal here would
// buy precision the inputs never had and imply a guarantee this system cannot
// make.
type Value struct {
	Kind Kind

	Number   float64
	Str      string
	Bool     bool
	Time     time.Time
	Duration Duration
	List     []Value
	Context  map[string]Value
}

// Duration is a FEEL duration. The two flavours are kept apart because they
// are not interchangeable: years and months have no fixed length, so a
// year-month duration cannot be reduced to nanoseconds without knowing the date
// it applies to.
type Duration struct {
	// YearMonth is true for P1Y2M-style durations, false for PT3H-style ones.
	YearMonth bool

	Months  int           // meaningful when YearMonth
	Elapsed time.Duration // meaningful when !YearMonth
	Neg     bool
}

// Null, True and False are the constant values, named so call sites read as
// prose rather than struct literals.
var (
	Null  = Value{Kind: KindNull}
	True  = Value{Kind: KindBoolean, Bool: true}
	False = Value{Kind: KindBoolean, Bool: false}
)

// Num builds a number value.
func Num(f float64) Value { return Value{Kind: KindNumber, Number: f} }

// Str builds a string value.
func Str(s string) Value { return Value{Kind: KindString, Str: s} }

// Bool builds a boolean value.
func Bool(b bool) Value {
	if b {
		return True
	}
	return False
}

// List builds a list value.
func List(items ...Value) Value { return Value{Kind: KindList, List: items} }

// IsNull reports whether v is the null value.
func (v Value) IsNull() bool { return v.Kind == KindNull }

// Truthy reports whether v is the boolean true.
//
// FEEL has no truthiness: only an actual boolean true is true. A non-boolean in
// a condition is an error, not a coincidence — which is why this returns the
// kind check rather than silently accepting a non-empty string.
func (v Value) Truthy() bool { return v.Kind == KindBoolean && v.Bool }

// String renders a value the way a person would write it, for error messages
// and for the plain-language narratives the product shows.
func (v Value) String() string {
	switch v.Kind {
	case KindNull:
		return "null"
	case KindNumber:
		if v.Number == math.Trunc(v.Number) && math.Abs(v.Number) < 1e15 {
			return fmt.Sprintf("%d", int64(v.Number))
		}
		return fmt.Sprintf("%g", v.Number)
	case KindString:
		return v.Str
	case KindBoolean:
		if v.Bool {
			return "true"
		}
		return "false"
	case KindDate:
		return v.Time.Format("2006-01-02")
	case KindTime:
		return v.Time.Format("15:04:05")
	case KindDateTime:
		return v.Time.Format(time.RFC3339)
	case KindDuration:
		return v.Duration.String()
	case KindList:
		parts := make([]string, len(v.List))
		for i, item := range v.List {
			parts[i] = item.String()
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case KindContext:
		return fmt.Sprintf("context with %d entries", len(v.Context))
	}
	return "?"
}

// String renders a duration in ISO-8601, which is how it was written.
func (d Duration) String() string {
	sign := ""
	if d.Neg {
		sign = "-"
	}
	if d.YearMonth {
		return fmt.Sprintf("%sP%dY%dM", sign, d.Months/12, d.Months%12)
	}

	rest := d.Elapsed
	days := int(rest / (24 * time.Hour))
	rest -= time.Duration(days) * 24 * time.Hour
	hours := int(rest / time.Hour)
	rest -= time.Duration(hours) * time.Hour
	minutes := int(rest / time.Minute)
	rest -= time.Duration(minutes) * time.Minute
	seconds := rest.Seconds()

	out := sign + "P"
	if days != 0 {
		out += fmt.Sprintf("%dD", days)
	}

	// The T separator only appears when something follows it: "P2DT" is not a
	// valid ISO-8601 duration, and round-tripping our own output has to work.
	timePart := ""
	if hours != 0 {
		timePart += fmt.Sprintf("%dH", hours)
	}
	if minutes != 0 {
		timePart += fmt.Sprintf("%dM", minutes)
	}
	if seconds != 0 {
		timePart += fmt.Sprintf("%gS", seconds)
	}
	if timePart != "" {
		out += "T" + timePart
	}
	// A duration of nothing still has to render as something valid.
	if out == sign+"P" {
		out += "T0S"
	}
	return out
}

// FromAny converts a Go value — typically decoded JSON from process variables —
// into a FEEL value.
//
// Unknown types become null rather than an error. A process variable holding
// something FEEL has no concept of is not an authoring mistake in the
// expression, and failing the whole evaluation over it would turn one odd
// variable into a stalled instance.
func FromAny(v any) Value {
	switch value := v.(type) {
	case nil:
		return Null
	case Value:
		return value
	case bool:
		return Bool(value)
	case string:
		return Str(value)
	case float64:
		return Num(value)
	case float32:
		return Num(float64(value))
	case int:
		return Num(float64(value))
	case int8:
		return Num(float64(value))
	case int16:
		return Num(float64(value))
	case int32:
		return Num(float64(value))
	case int64:
		return Num(float64(value))
	case uint:
		return Num(float64(value))
	case uint8:
		return Num(float64(value))
	case uint16:
		return Num(float64(value))
	case uint32:
		return Num(float64(value))
	case uint64:
		return Num(float64(value))
	case time.Time:
		return Value{Kind: KindDateTime, Time: value}
	case time.Duration:
		return Value{Kind: KindDuration, Duration: Duration{Elapsed: value, Neg: value < 0}}
	case []any:
		items := make([]Value, len(value))
		for i, item := range value {
			items[i] = FromAny(item)
		}
		return Value{Kind: KindList, List: items}
	case map[string]any:
		entries := make(map[string]Value, len(value))
		for key, item := range value {
			entries[key] = FromAny(item)
		}
		return Value{Kind: KindContext, Context: entries}
	}
	return Null
}

// ToAny converts a FEEL value back into a plain Go value, for storing in
// process variables and serialising to JSON.
func (v Value) ToAny() any {
	switch v.Kind {
	case KindNull:
		return nil
	case KindNumber:
		return v.Number
	case KindString:
		return v.Str
	case KindBoolean:
		return v.Bool
	case KindDate, KindTime, KindDateTime:
		return v.Time
	case KindDuration:
		if v.Duration.YearMonth {
			return v.Duration.String()
		}
		return v.Duration.Elapsed
	case KindList:
		items := make([]any, len(v.List))
		for i, item := range v.List {
			items[i] = item.ToAny()
		}
		return items
	case KindContext:
		entries := make(map[string]any, len(v.Context))
		for key, item := range v.Context {
			entries[key] = item.ToAny()
		}
		return entries
	}
	return nil
}
