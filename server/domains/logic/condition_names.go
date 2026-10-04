package logic

import (
	"maps"
	"slices"
	"strings"

	"github.com/gsoultan/metis/server/domains/logic/feel"
)

// ReferencedNames reports which variables a condition reads, by name, and
// whether that could be told at all. The names are sorted and each is listed
// once.
//
// It exists so that a step can be waived without a decision point being left
// to decide from a value nobody supplied. So every doubt goes one way: a
// condition this cannot read is not analysable — never one that reads nothing.
// A name too many costs a question; a name too few costs a branch taken on a
// value that was never there.
//
// It follows the evaluator chain (GetConditionEvaluatorChain), link by link:
//
//   - an empty condition reads nothing;
//   - a js: condition is JavaScript, which nothing here reads;
//   - name=value reads the name, and when the name is unset the chain hands
//     the condition on to FEEL, which reads the right side as a variable where
//     one is set — so it reads whatever FEEL reads in it too;
//   - anything else is FEEL, or cannot be read.
//
// In FEEL a name is a variable, a path is read at its root (`order.total`
// reads order), a function's name is never a variable though its arguments
// are, a context's keys are its own, and what is inside quotes is text.
//
// One thing the chain reads is not reported: before it parses a condition it
// looks its whole text up as the name of a boolean variable
// (SimpleVariableEvaluator). That is a question about a particular set of
// names, and whoever holds the set asks it; the names here stay the
// identifiers a person would point at.
func ReferencedNames(condition string) (names []string, analysable bool) {
	condition = strings.TrimSpace(condition)
	switch {
	case condition == "":
		return nil, true
	case strings.HasPrefix(condition, "js:"):
		return nil, false
	}

	read := map[string]struct{}{}
	key, _, plain := plainEquality(condition)
	if plain && key != "" {
		read[key] = struct{}{}
	}
	parsed, analysable := feelNames(condition, read)
	if !parsed {
		// A plain equality FEEL cannot parse is still fully read: with its name
		// unset the chain's last link fails and answers false. Anything else
		// FEEL cannot parse is a condition nothing can read.
		return sortedNames(read), plain
	}
	return sortedNames(read), analysable
}

// feelNames adds the variables a FEEL expression reads to into. It reports
// whether the text parsed as an expression, and whether everything in it could
// be read.
func feelNames(expression string, into map[string]struct{}) (parsed, analysable bool) {
	tree, err := feel.Parse(expression)
	if err != nil {
		return false, false
	}
	return true, collectNames(tree, into)
}

// collectNames adds the variables a parsed expression reads to into, and
// reports whether every node in it was one it knows how to read.
//
// The switch names every node the parser makes. The default is what matters:
// a node added to the language later is not analysable until it is given a
// case here, rather than quietly read as naming nothing.
//
// It recurses, and is bounded by the parser, which refuses an expression
// nested deeper than it allows.
func collectNames(node feel.Node, into map[string]struct{}) bool {
	switch n := node.(type) {
	case *feel.Literal:
		return true
	case *feel.Name:
		if n != nil {
			into[n.Text] = struct{}{}
		}
		return true
	case *feel.Path:
		return n == nil || collectNames(n.Target, into)
	case *feel.Index:
		return n == nil || collectAll(into, n.Target, n.Index)
	case *feel.Binary:
		return n == nil || collectAll(into, n.Left, n.Right)
	case *feel.Unary:
		return n == nil || collectNames(n.Operand, into)
	case *feel.RangeNode:
		return n == nil || collectAll(into, n.Low, n.High)
	case *feel.ListNode:
		return n == nil || collectAll(into, n.Items...)
	case *feel.ContextNode:
		return n == nil || collectAll(into, n.Values...)
	case *feel.Call:
		return n == nil || collectAll(into, n.Args...)
	case *feel.If:
		return n == nil || collectAll(into, n.Cond, n.Then, n.Else)
	case *feel.InNode:
		return n == nil || collectAll(into, n.Value, n.Target)
	case *feel.UnaryTest:
		return n == nil || collectNames(n.Expr, into)
	case *feel.UnaryTests:
		return n == nil || collectAll(into, n.Tests...)
	}
	return false
}

// collectAll collects from every node, and reports whether all could be read.
// It does not stop at the first that could not: the names that can be read
// are still worth having.
func collectAll(into map[string]struct{}, nodes ...feel.Node) bool {
	readable := true
	for _, node := range nodes {
		readable = collectNames(node, into) && readable
	}
	return readable
}

// sortedNames is a set of names as a sorted list, nil when it is empty.
func sortedNames(set map[string]struct{}) []string {
	if len(set) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(set))
}
