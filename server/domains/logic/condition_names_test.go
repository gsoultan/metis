package logic

import (
	"maps"
	"reflect"
	"strings"
	"testing"

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
		{"=done", nil, true},

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
