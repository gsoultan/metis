package feel

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// InputName is the variable a decision-table cell compares against. The table
// evaluator binds the row's input value to it before testing each cell.
const InputName = "_input"

// Scope holds the variables an expression can see.
type Scope map[string]Value

// NewScope converts process variables into a scope.
func NewScope(vars map[string]any) Scope {
	scope := make(Scope, len(vars))
	for name, value := range vars {
		scope[name] = FromAny(value)
	}
	return scope
}

// Eval evaluates a parsed expression.
func Eval(node Node, scope Scope) (Value, error) {
	e := &evaluator{scope: scope}
	return e.eval(node)
}

type evaluator struct {
	scope Scope
	depth int
}

func (e *evaluator) eval(node Node) (Value, error) {
	// The parser already bounds nesting, but evaluation recurses over the same
	// tree and a bound here costs nothing.
	if e.depth++; e.depth > maxDepth*2 {
		return Null, fmt.Errorf("feel: expression is too deeply nested to evaluate")
	}
	defer func() { e.depth-- }()

	switch n := node.(type) {
	case *Literal:
		return n.Value, nil

	case *Name:
		value, ok := e.scope[n.Text]
		if !ok {
			// An unknown name is null, per the specification. It is not an
			// error: a decision table routinely tests inputs a given instance
			// never set, and failing there would turn a missing optional
			// variable into a stalled process.
			return Null, nil
		}
		return value, nil

	case *Path:
		return e.evalPath(n)

	case *Index:
		return e.evalIndex(n)

	case *Unary:
		return e.evalUnary(n)

	case *Binary:
		return e.evalBinary(n)

	case *ListNode:
		items := make([]Value, len(n.Items))
		for i, item := range n.Items {
			value, err := e.eval(item)
			if err != nil {
				return Null, err
			}
			items[i] = value
		}
		return Value{Kind: KindList, List: items}, nil

	case *ContextNode:
		entries := make(map[string]Value, len(n.Keys))
		for i, key := range n.Keys {
			value, err := e.eval(n.Values[i])
			if err != nil {
				return Null, err
			}
			entries[key] = value
		}
		return Value{Kind: KindContext, Context: entries}, nil

	case *RangeNode:
		// A bare range evaluates to itself only in a test position; as a value
		// it is meaningless, so it is represented as a two-item list only when
		// asked to contain something. Containment is handled by InNode and
		// UnaryTest, so reaching here means the range was used as a value.
		return Null, fmt.Errorf("feel: a range can only be used in a test, not as a value")

	case *Call:
		return e.evalCall(n)

	case *If:
		cond, err := e.eval(n.Cond)
		if err != nil {
			return Null, err
		}
		if cond.Truthy() {
			return e.eval(n.Then)
		}
		return e.eval(n.Else)

	case *InNode:
		return e.evalIn(n)

	case *UnaryTest:
		return e.evalUnaryTest(n)

	case *UnaryTests:
		return e.evalUnaryTests(n)
	}

	return Null, fmt.Errorf("feel: cannot evaluate %T", node)
}

func (e *evaluator) evalPath(n *Path) (Value, error) {
	target, err := e.eval(n.Target)
	if err != nil {
		return Null, err
	}
	switch target.Kind {
	case KindContext:
		if value, ok := target.Context[n.Field]; ok {
			return value, nil
		}
		return Null, nil
	case KindDate, KindDateTime:
		return dateProperty(target, n.Field)
	case KindList:
		// Projection: `items.price` collects the field from every entry, which
		// is what makes `sum(items.price)` work.
		out := make([]Value, 0, len(target.List))
		for _, item := range target.List {
			if item.Kind == KindContext {
				out = append(out, item.Context[n.Field])
			}
		}
		return Value{Kind: KindList, List: out}, nil
	case KindNull:
		return Null, nil
	}
	return Null, fmt.Errorf("feel: %s has no property %q", target.Kind, n.Field)
}

// dateProperty exposes the date components FEEL defines as properties.
func dateProperty(v Value, field string) (Value, error) {
	switch field {
	case "year":
		return Num(float64(v.Time.Year())), nil
	case "month":
		return Num(float64(v.Time.Month())), nil
	case "day":
		return Num(float64(v.Time.Day())), nil
	case "hour":
		return Num(float64(v.Time.Hour())), nil
	case "minute":
		return Num(float64(v.Time.Minute())), nil
	case "second":
		return Num(float64(v.Time.Second())), nil
	case "weekday":
		// FEEL numbers weekdays 1..7 from Monday; Go's Sunday is 0.
		day := int(v.Time.Weekday())
		if day == 0 {
			day = 7
		}
		return Num(float64(day)), nil
	}
	return Null, fmt.Errorf("feel: a date has no property %q", field)
}

func (e *evaluator) evalIndex(n *Index) (Value, error) {
	target, err := e.eval(n.Target)
	if err != nil {
		return Null, err
	}
	index, err := e.eval(n.Index)
	if err != nil {
		return Null, err
	}
	if target.Kind != KindList {
		return Null, fmt.Errorf("feel: cannot index a %s", target.Kind)
	}
	if index.Kind != KindNumber {
		return Null, fmt.Errorf("feel: list index must be a number, found %s", index.Kind)
	}

	// FEEL indexes from 1; a negative index counts back from the end.
	i := int(index.Number)
	switch {
	case i > 0 && i <= len(target.List):
		return target.List[i-1], nil
	case i < 0 && -i <= len(target.List):
		return target.List[len(target.List)+i], nil
	}
	return Null, nil
}

func (e *evaluator) evalUnary(n *Unary) (Value, error) {
	operand, err := e.eval(n.Operand)
	if err != nil {
		return Null, err
	}
	switch n.Op {
	case "-":
		if operand.Kind != KindNumber {
			return Null, fmt.Errorf("feel: cannot negate a %s", operand.Kind)
		}
		return Num(-operand.Number), nil
	case "not":
		if operand.Kind != KindBoolean {
			return Null, fmt.Errorf("feel: not() needs a boolean, found %s", operand.Kind)
		}
		return Bool(!operand.Bool), nil
	}
	return Null, fmt.Errorf("feel: unknown operator %q", n.Op)
}

func (e *evaluator) evalBinary(n *Binary) (Value, error) {
	// `and` and `or` short-circuit, so the right side is not evaluated when the
	// left decides the answer.
	switch n.Op {
	case "and":
		left, err := e.eval(n.Left)
		if err != nil {
			return Null, err
		}
		if !left.Truthy() {
			return False, nil
		}
		right, err := e.eval(n.Right)
		if err != nil {
			return Null, err
		}
		return Bool(right.Truthy()), nil

	case "or":
		left, err := e.eval(n.Left)
		if err != nil {
			return Null, err
		}
		if left.Truthy() {
			return True, nil
		}
		right, err := e.eval(n.Right)
		if err != nil {
			return Null, err
		}
		return Bool(right.Truthy()), nil
	}

	left, err := e.eval(n.Left)
	if err != nil {
		return Null, err
	}
	right, err := e.eval(n.Right)
	if err != nil {
		return Null, err
	}

	switch n.Op {
	case "=":
		return Bool(equal(left, right)), nil
	case "!=":
		return Bool(!equal(left, right)), nil
	case "<", "<=", ">", ">=":
		return compareOp(n.Op, left, right)
	case "+", "-", "*", "/", "**":
		return arithmetic(n.Op, left, right)
	}
	return Null, fmt.Errorf("feel: unknown operator %q", n.Op)
}

// equal compares two values for FEEL equality. Different types are never equal,
// which is the point: "1" = 1 is false, where the string matcher this replaces
// said true because it compared their printed forms.
func equal(a, b Value) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case KindNull:
		return true
	case KindNumber:
		return a.Number == b.Number
	case KindString:
		return a.Str == b.Str
	case KindBoolean:
		return a.Bool == b.Bool
	case KindDate, KindTime, KindDateTime:
		return a.Time.Equal(b.Time)
	case KindDuration:
		if a.Duration.YearMonth != b.Duration.YearMonth {
			return false
		}
		if a.Duration.YearMonth {
			return a.Duration.Months == b.Duration.Months
		}
		return a.Duration.Elapsed == b.Duration.Elapsed
	case KindList:
		if len(a.List) != len(b.List) {
			return false
		}
		for i := range a.List {
			if !equal(a.List[i], b.List[i]) {
				return false
			}
		}
		return true
	case KindContext:
		if len(a.Context) != len(b.Context) {
			return false
		}
		for key, value := range a.Context {
			other, ok := b.Context[key]
			if !ok || !equal(value, other) {
				return false
			}
		}
		return true
	}
	return false
}

// order returns -1, 0 or 1 for values that have a natural order.
func order(a, b Value) (int, error) {
	if a.Kind != b.Kind {
		return 0, fmt.Errorf("feel: cannot compare a %s with a %s", a.Kind, b.Kind)
	}
	switch a.Kind {
	case KindNumber:
		switch {
		case a.Number < b.Number:
			return -1, nil
		case a.Number > b.Number:
			return 1, nil
		}
		return 0, nil
	case KindString:
		return strings.Compare(a.Str, b.Str), nil
	case KindDate, KindTime, KindDateTime:
		switch {
		case a.Time.Before(b.Time):
			return -1, nil
		case a.Time.After(b.Time):
			return 1, nil
		}
		return 0, nil
	case KindDuration:
		if a.Duration.YearMonth != b.Duration.YearMonth {
			return 0, fmt.Errorf("feel: cannot compare a year-month duration with a day-time one")
		}
		left, right := a.Duration.Elapsed, b.Duration.Elapsed
		if a.Duration.YearMonth {
			left = time.Duration(a.Duration.Months)
			right = time.Duration(b.Duration.Months)
		}
		switch {
		case left < right:
			return -1, nil
		case left > right:
			return 1, nil
		}
		return 0, nil
	}
	return 0, fmt.Errorf("feel: a %s has no ordering", a.Kind)
}

func compareOp(op string, a, b Value) (Value, error) {
	// Comparing with null is false rather than an error: a decision table
	// testing an input the instance never set should not fail the evaluation.
	if a.IsNull() || b.IsNull() {
		return False, nil
	}
	cmp, err := order(a, b)
	if err != nil {
		return Null, err
	}
	switch op {
	case "<":
		return Bool(cmp < 0), nil
	case "<=":
		return Bool(cmp <= 0), nil
	case ">":
		return Bool(cmp > 0), nil
	case ">=":
		return Bool(cmp >= 0), nil
	}
	return Null, fmt.Errorf("feel: unknown comparison %q", op)
}

func arithmetic(op string, a, b Value) (Value, error) {
	// String concatenation with +, which FEEL allows.
	if op == "+" && a.Kind == KindString && b.Kind == KindString {
		return Str(a.Str + b.Str), nil
	}

	// Date/duration arithmetic: the operations that make timers expressible.
	if value, ok, err := temporalArithmetic(op, a, b); ok {
		return value, err
	}

	if a.Kind != KindNumber || b.Kind != KindNumber {
		return Null, fmt.Errorf("feel: cannot apply %s to a %s and a %s", op, a.Kind, b.Kind)
	}
	switch op {
	case "+":
		return Num(a.Number + b.Number), nil
	case "-":
		return Num(a.Number - b.Number), nil
	case "*":
		return Num(a.Number * b.Number), nil
	case "/":
		if b.Number == 0 {
			return Null, fmt.Errorf("feel: division by zero")
		}
		return Num(a.Number / b.Number), nil
	case "**":
		return Num(math.Pow(a.Number, b.Number)), nil
	}
	return Null, fmt.Errorf("feel: unknown operator %q", op)
}

// temporalArithmetic handles date ± duration and date − date. The bool reports
// whether the operands were temporal at all.
func temporalArithmetic(op string, a, b Value) (Value, bool, error) {
	isTime := func(v Value) bool {
		return v.Kind == KindDate || v.Kind == KindDateTime || v.Kind == KindTime
	}

	switch {
	case isTime(a) && b.Kind == KindDuration && (op == "+" || op == "-"):
		delta := b.Duration
		if op == "-" {
			delta.Neg = !delta.Neg
		}
		return Value{Kind: a.Kind, Time: addDuration(a.Time, delta)}, true, nil

	case a.Kind == KindDuration && isTime(b) && op == "+":
		return Value{Kind: b.Kind, Time: addDuration(b.Time, a.Duration)}, true, nil

	case isTime(a) && isTime(b) && op == "-":
		return Value{Kind: KindDuration, Duration: Duration{
			Elapsed: a.Time.Sub(b.Time),
			Neg:     a.Time.Before(b.Time),
		}}, true, nil

	case a.Kind == KindDuration && b.Kind == KindDuration && (op == "+" || op == "-"):
		if a.Duration.YearMonth != b.Duration.YearMonth {
			return Null, true, fmt.Errorf("feel: cannot add a year-month duration to a day-time one")
		}
		sign := 1
		if op == "-" {
			sign = -1
		}
		if a.Duration.YearMonth {
			return Value{Kind: KindDuration, Duration: Duration{
				YearMonth: true,
				Months:    signedMonths(a.Duration) + sign*signedMonths(b.Duration),
			}}, true, nil
		}
		total := signedElapsed(a.Duration) + time.Duration(sign)*signedElapsed(b.Duration)
		return Value{Kind: KindDuration, Duration: Duration{Elapsed: absDuration(total), Neg: total < 0}}, true, nil
	}
	return Null, false, nil
}

func addDuration(t time.Time, d Duration) time.Time {
	if d.YearMonth {
		months := signedMonths(d)
		return t.AddDate(0, months, 0)
	}
	return t.Add(signedElapsed(d))
}

func signedMonths(d Duration) int {
	if d.Neg {
		return -d.Months
	}
	return d.Months
}

func signedElapsed(d Duration) time.Duration {
	if d.Neg {
		return -d.Elapsed
	}
	return d.Elapsed
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func (e *evaluator) evalIn(n *InNode) (Value, error) {
	value, err := e.eval(n.Value)
	if err != nil {
		return Null, err
	}
	return e.testAgainst(value, n.Target)
}

// testAgainst is the shared membership check behind `in`, ranges and
// decision-table cells.
func (e *evaluator) testAgainst(input Value, target Node) (Value, error) {
	if rangeNode, ok := target.(*RangeNode); ok {
		return e.inRange(input, rangeNode)
	}

	value, err := e.eval(target)
	if err != nil {
		return Null, err
	}
	if value.Kind == KindList {
		for _, item := range value.List {
			if equal(input, item) {
				return True, nil
			}
		}
		return False, nil
	}
	return Bool(equal(input, value)), nil
}

func (e *evaluator) inRange(input Value, r *RangeNode) (Value, error) {
	low, err := e.eval(r.Low)
	if err != nil {
		return Null, err
	}
	high, err := e.eval(r.High)
	if err != nil {
		return Null, err
	}
	if input.IsNull() {
		return False, nil
	}

	lowCmp, err := order(input, low)
	if err != nil {
		return Null, err
	}
	highCmp, err := order(input, high)
	if err != nil {
		return Null, err
	}

	lowOK := lowCmp > 0 || (!r.LowOpen && lowCmp == 0)
	highOK := highCmp < 0 || (!r.HighOpen && highCmp == 0)
	return Bool(lowOK && highOK), nil
}

func (e *evaluator) evalUnaryTest(n *UnaryTest) (Value, error) {
	input := e.scope[InputName]

	if n.Op == "" {
		// A bare word in a decision cell is read as text when no variable of
		// that name is in scope.
		//
		// Strict FEEL says a bare name is always a variable reference, and
		// Camunda requires cells to quote their strings. Real tables in this
		// codebase — and the ones business users write — say CLOSED, not
		// "CLOSED", because a cell reading `CLOSED` obviously means the status.
		// Enforcing the strict rule would silently turn every such cell into
		// null and stop matching, which for a deployed decision table means
		// wrong answers rather than an error someone would notice.
		//
		// A variable of that name still wins, so `> threshold` and
		// `otherInput` keep their FEEL meaning. The ambiguity only resolves
		// toward text when there is nothing to resolve toward.
		if name, ok := n.Expr.(*Name); ok {
			if _, inScope := e.scope[name.Text]; !inScope {
				return Bool(equal(input, Str(name.Text))), nil
			}
		}
		return e.testAgainst(input, n.Expr)
	}

	expected, err := e.eval(n.Expr)
	if err != nil {
		return Null, err
	}
	switch n.Op {
	case "=":
		return Bool(equal(input, expected)), nil
	case "!=":
		return Bool(!equal(input, expected)), nil
	}

	result, err := compareOp(n.Op, input, expected)
	//nolint:nilerr // Discarding this error is the behaviour, not an oversight: see below.
	if err != nil {
		// A cell compares against whatever the instance actually supplied, and
		// a string arriving where the table expects a number is a data
		// condition rather than an authoring mistake — the row simply does not
		// match. DMN says the same: an incomparable pair yields null, and null
		// in a test position is no match. Genuinely broken cells still fail, at
		// parse time, before any instance runs.
		return False, nil
	}
	return result, nil
}

func (e *evaluator) evalUnaryTests(n *UnaryTests) (Value, error) {
	if n.MatchAll {
		return True, nil
	}

	matched := false
	for _, test := range n.Tests {
		result, err := e.eval(test)
		if err != nil {
			return Null, err
		}
		if result.Truthy() {
			matched = true
			break
		}
	}
	if n.Negated {
		return Bool(!matched), nil
	}
	return Bool(matched), nil
}
