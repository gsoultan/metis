package logic

import (
	"maps"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic/feel"
)

// What a condition reads decides whether waiving a step leaves a decision
// point with nothing to decide from. The rules mirror the evaluator chain:
// empty reads nothing, js: cannot be read, name=value reads the name and —
// because the chain hands it to FEEL when the name is unset — whatever FEEL
// reads in it, and anything else is FEEL.
//
// Every doubt goes one way: what cannot be read is "not analysable", never
// "reads nothing".
func TestReferencedNames(t *testing.T) {
	t.Parallel()
	cases := []struct {
		condition  string
		names      []string
		analysable bool
	}{
		{"", nil, true},
		{"   ", nil, true},
		{"js:approved === true", nil, false},
		{"  js:approved === true", nil, false},
		{"approved", []string{"approved"}, true},
		{"  approved  ", []string{"approved"}, true},

		// name=value. The chain reads the right side as a variable when the left
		// is unset, and a waived step is exactly when a name is unset.
		{"status=done", []string{"done", "status"}, true},
		{"approved = false", []string{"approved"}, true},
		{"level = 3", []string{"level"}, true},
		{"total=net+tax", []string{"net", "tax", "total"}, true},
		// FEEL cannot read this one, so when the name is unset it answers false
		// and reads nothing more: the left side is all there is.
		{"status=done now", []string{"status"}, true},
		{"status=in progress", []string{"status"}, true},
		// The chain looks up whatever is left of the equals sign, and with
		// nothing there it looks up the variable whose name is empty.
		{"=done", []string{""}, true},
		{"=", []string{""}, true},
		// Not a template: the braces are part of the name the chain looks up.
		{"${approved}=true", []string{"${approved}"}, true},

		{"amount > 100 and approved", []string{"amount", "approved"}, true},
		{"amount > 1 and amount < 5", []string{"amount"}, true},
		{"not(approved) or escalate", []string{"approved", "escalate"}, true},
		{"not(approved)", []string{"approved"}, true},
		{"-debt < floor", []string{"debt", "floor"}, true},
		{"x in [1..3]", []string{"x"}, true},
		{"amount in ]low..high]", []string{"amount", "high", "low"}, true},
		{"age between low and high", []string{"age", "high", "low"}, true},
		{"tier in [gold, \"SILVER\"]", []string{"gold", "tier"}, true},
		{"if approved then limit else 0", []string{"approved", "limit"}, true},
		{"items[1] = reviewer", []string{"items", "reviewer"}, true},
		{"items[position] > 2", []string{"items", "position"}, true},

		// A path reads the variable at its root; what follows the dot is a
		// property of that value, not a name of its own.
		{"applicant.income > 5000", []string{"applicant"}, true},
		{"order.total > limit", []string{"limit", "order"}, true},
		// No quote and no operator but the equals sign, so the chain first takes
		// the whole left side as one variable's name, and FEEL reads the path.
		{"order.customer.address.city = home", []string{"home", "order", "order.customer.address.city"}, true},
		{"sum(items.price) > budget", []string{"budget", "items"}, true},

		// A function's name is never a variable; its arguments are.
		{"count(items) > 2", []string{"items"}, true},
		{"date(dueDate) < today()", []string{"dueDate"}, true},
		{"starts with(name, \"A\")", []string{"name"}, true},
		{"string length(code) > minimum", []string{"code", "minimum"}, true},
		{"contains(upper case(note), keyword)", []string{"keyword", "note"}, true},

		// A name inside a string is text.
		{"status = \"approved\"", []string{"status"}, true},
		{"region = 'EU'", []string{"region"}, true},
		{"contains(note, \"approved and amount > 100\")", []string{"note"}, true},
		{"\"approved\" = \"approved\"", nil, true},

		// Keywords and constants are not names.
		{"true", nil, true},
		{"1 < 2", nil, true},
		{"outcome != null", []string{"outcome"}, true},

		// A context's keys are its own; its values are read.
		{"{limit: cap}.limit > spend", []string{"cap", "spend"}, true},

		// What cannot be read is not analysable.
		{"((", nil, false},
		{"amount >", nil, false},
		{"first name", nil, false},
		// Spaced, " in " makes it more than name=value to the chain, so it is
		// FEEL's alone, and FEEL cannot read it.
		{"status = in progress", nil, false},
		{"approved ? yes : no", nil, false},
		// Other engines' ways of writing a variable are not this one's.
		{"${approved}", nil, false},
		{"${approved} == true", nil, false},
		{"#{approved}", nil, false},
		{"`approved`", nil, false},
		{"`first name` = \"Ann\"", nil, false},
		{"for x in xs return x", nil, false},
		{"some x in xs satisfies x > 1", nil, false},
		{"x instance of number", nil, false},
		{"amount > 100 and \"unterminated", nil, false},
		{strings.Repeat("not(", 100) + "approved" + strings.Repeat(")", 100), nil, false},
	}
	for _, c := range cases {
		names, analysable := ReferencedNames(c.condition)
		if analysable != c.analysable || !reflect.DeepEqual(names, c.names) {
			t.Errorf("ReferencedNames(%q) = %v, %v; want %v, %v", c.condition, names, analysable, c.names, c.analysable)
		}
	}
}

// The chain must agree with what ReferencedNames says it reads: a condition
// said to read only some names answers the same whatever any other variable
// holds. This is the property the waive relies on, checked against the
// evaluator itself rather than against the table above.
func TestTheChainReadsNoIdentifierReferencedNamesLeavesOut(t *testing.T) {
	t.Parallel()
	conditions := []string{
		"status=done", "approved = false", "amount > 100 and approved", "total=net+tax",
		"items[1] = reviewer", "if approved then limit > 3 else false", "tier in [gold, \"SILVER\"]",
		"order.total > limit", "status = \"approved\"", "contains(note, \"approved\")",
		"age between low and high", "not(approved) or escalate",
	}
	// Every identifier any of the conditions could mean, each with a value that
	// changes an answer if it is read: "done" holds the word the left side of
	// status=done is read as when status is unset.
	everything := map[string]any{
		"status": "open", "done": "status", "approved": true, "amount": 500.0, "total": 7.0, "net": 3.0, "tax": 4.0,
		"items": []any{"ann"}, "reviewer": "ann", "limit": 9.0, "tier": "GOLD", "gold": "GOLD",
		"order": map[string]any{"total": 10.0}, "note": "approved by ann", "age": 30.0, "low": 18.0, "high": 65.0,
		"escalate": false,
	}
	chain := GetConditionEvaluatorChain()
	for _, condition := range conditions {
		names, analysable := ReferencedNames(condition)
		if !analysable {
			t.Errorf("%q: not analysable", condition)
			continue
		}
		read, rest := map[string]any{}, maps.Clone(everything)
		for _, name := range names {
			if value, set := everything[name]; set {
				read[name] = value
			}
			delete(rest, name)
		}
		if with, without := chain.Evaluate(condition, everything), chain.Evaluate(condition, read); with != without {
			t.Errorf("%q answers %v with every variable and %v with only %v: it reads a name that was left out",
				condition, with, without, names)
		}
		// With every name it reads unset — a waived step — nothing else may
		// decide it either.
		if others, none := chain.Evaluate(condition, rest), chain.Evaluate(condition, map[string]any{}); others != none {
			t.Errorf("%q answers %v when only the names it does not read are set and %v when nothing is: it reads one of them",
				condition, others, none)
		}
	}
}

// A tree holding something this does not know how to read must not be taken
// to read nothing. No expression parses to one today; the day the parser
// learns a new construct, this is what keeps it from being read as empty.
func TestAnUnknownNodeIsNotAnalysable(t *testing.T) {
	t.Parallel()
	into := map[string]struct{}{}
	if collectNames(nil, into) {
		t.Error("a node of no known type was read as reading nothing")
	}
	if collectNames(&feel.Binary{Op: "and", Left: &feel.Name{Text: "approved"}, Right: nil}, into) {
		t.Error("a tree with an unknown node in it was read as analysable")
	}
	// A nil of a known type holds nothing, and says so.
	if !collectNames((*feel.Name)(nil), into) || !collectNames((*feel.Call)(nil), into) {
		t.Error("a nil node of a known type is not analysable")
	}
	if _, read := into["approved"]; !read || len(into) != 1 {
		t.Errorf("names collected: %v", into)
	}
}

// tooDeep reports whether the evaluator refused an expression for its depth.
func tooDeep(err error) bool {
	return err != nil && strings.Contains(err.Error(), "too deeply nested")
}

// chains are the shapes that are as deep as they are long: the parser builds
// each to the left without counting it as nesting, so its own bound does not
// stop them. Each is given how many levels deep its deepest name sits.
var chains = map[string]func(levels int) string{
	"a sum":   func(levels int) string { return strings.Repeat("a+", levels-1) + "a" },
	"a path":  func(levels int) string { return "a" + strings.Repeat(".b", levels-1) },
	"indexes": func(levels int) string { return "a" + strings.Repeat("[1]", levels-1) },
	// A range under `in` is tested, not evaluated: its ends sit at the level
	// of the value beside it, one below the test.
	"a range at the bottom": func(levels int) string { return "(x in [low..high])" + strings.Repeat("+a", levels-2) },
}

// The evaluator refuses an expression nested deeper than it allows, so a
// condition that deep always answers false and nobody can say what it would
// have read. Reading the names stops at the same depth and says so — and the
// two must agree on where that is, level for level.
func TestNamesAreReadAsDeepAsTheEvaluatorEvaluatesAndNoDeeper(t *testing.T) {
	t.Parallel()
	vars := map[string]any{"a": map[string]any{}, "x": 1.0, "low": 0.0, "high": 2.0}
	for shape, build := range chains {
		refusedSomewhere, readSomewhere := false, false
		for levels := 120; levels <= 136; levels++ {
			expression := build(levels)
			_, err := feel.Evaluate(expression, vars)
			_, analysable := ReferencedNames(expression)
			if analysable == tooDeep(err) {
				t.Errorf("%s, %d levels: the evaluator says %v and the names are analysable: %v", shape, levels, err, analysable)
			}
			refusedSomewhere = refusedSomewhere || tooDeep(err)
			readSomewhere = readSomewhere || !tooDeep(err)
		}
		if !refusedSomewhere || !readSomewhere {
			t.Errorf("%s: the evaluator's limit is not between 120 and 136 levels; this test no longer straddles it", shape)
		}
	}
}

// A definition is somebody's input, and so is every condition in it. A
// megabyte of `a+a+a+…` is a tree a million levels deep: reading it must not
// descend a million calls, which is a stack no request should be given. The
// stack is held to 64 MiB here so that descending it is fatal rather than
// merely enormous.
//
// Only the reading of the names is timed. Parsing a megabyte takes what it
// takes, and the engine pays that too.
//
// Not parallel: the stack limit is the process's.
func TestAConditionAMillionLevelsDeepIsNotDescended(t *testing.T) {
	defer debug.SetMaxStack(debug.SetMaxStack(64 << 20))
	const levels = 1 << 20
	for shape, build := range chains {
		expression := build(levels)
		tree, err := feel.Parse(expression)
		if err != nil {
			t.Fatalf("%s: %v", shape, err)
		}
		started := time.Now()
		readable := collectNames(tree, map[string]struct{}{})
		if elapsed := time.Since(started); readable || elapsed > time.Second {
			t.Errorf("%s, %d levels: read as analysable %v in %s; want a refusal at once", shape, levels, readable, elapsed)
		}
		if _, analysable := ReferencedNames(expression); analysable {
			t.Errorf("%s, %d levels: analysable as a condition", shape, levels)
		}
	}

	// The same reading serves a decision table's columns and cells and a
	// mapping's sources.
	expression := chains["a sum"](levels)
	_, _, table := DecisionTableReads(entities.DecisionDefinition{
		Inputs: []entities.DecisionInput{{Expression: "amount"}},
		Rules:  []entities.DecisionRule{{Inputs: []string{"> " + expression}}},
	})
	_, _, column := DecisionTableReads(entities.DecisionDefinition{Inputs: []entities.DecisionInput{{Expression: expression}}})
	_, mapping := MappingSourceNames(map[string]any{"total": expression})
	if table || column || mapping {
		t.Errorf("%d levels: analysable in a cell %v, as a column %v, as a mapping source %v; want none", levels, table, column, mapping)
	}
}
