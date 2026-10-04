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
	if plain {
		// Whatever is left of the equals sign is looked up, and with nothing
		// there — `=done` — that is the variable whose name is empty. It is
		// reported like any other: a field may be named anything.
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

// maxReadDepth is how many levels of a parsed expression are read for names.
//
// It is the evaluator's own limit: feel refuses to evaluate an expression
// nested deeper than this (eval.go, maxDepth*2), so a condition that deep
// always answers false, and nothing can say what it would have read. The
// parser's limit does not stand in for it — that one bounds brackets, and the
// parser builds `a+a+a+…`, `a.b.b.b…` and `a[1][1]…` to the left without
// counting them, so each is as deep as it is long.
//
// The evaluator's limit is not exported, and this package may not reach into
// it; TestNamesAreReadAsDeepAsTheEvaluatorEvaluatesAndNoDeeper holds the two
// together, level for level.
const maxReadDepth = 128

// collectNames adds the variables a parsed expression reads to into, and
// reports whether all of it could be read: every node one it knows, and none
// deeper than the evaluator evaluates.
func collectNames(node feel.Node, into map[string]struct{}) bool {
	return namesAt(node, into, 1)
}

// namesAt is collectNames for a node the evaluator would reach at depth.
//
// The switch names every node the parser makes. The default is what matters:
// a node added to the language later is not analysable until it is given a
// case here, rather than quietly read as naming nothing.
//
// It recurses, one call a level, and stops at maxReadDepth: an expression is
// somebody's input, and one a million levels deep must cost a refusal, not a
// million frames. Depth is counted as the evaluator counts it, which is one
// level a node except for a range: a range is tested rather than evaluated,
// so its ends sit at its own level.
func namesAt(node feel.Node, into map[string]struct{}, depth int) bool {
	if depth > maxReadDepth {
		return false
	}
	below := depth + 1
	switch n := node.(type) {
	case *feel.Literal:
		return true
	case *feel.Name:
		if n != nil {
			into[n.Text] = struct{}{}
		}
		return true
	case *feel.Path:
		return n == nil || namesAt(n.Target, into, below)
	case *feel.Index:
		return n == nil || namesOfAll(into, below, n.Target, n.Index)
	case *feel.Binary:
		return n == nil || namesOfAll(into, below, n.Left, n.Right)
	case *feel.Unary:
		return n == nil || namesAt(n.Operand, into, below)
	case *feel.RangeNode:
		return n == nil || namesOfAll(into, depth, n.Low, n.High)
	case *feel.ListNode:
		return n == nil || namesOfAll(into, below, n.Items...)
	case *feel.ContextNode:
		return n == nil || namesOfAll(into, below, n.Values...)
	case *feel.Call:
		return n == nil || namesOfAll(into, below, n.Args...)
	case *feel.If:
		return n == nil || namesOfAll(into, below, n.Cond, n.Then, n.Else)
	case *feel.InNode:
		return n == nil || namesOfAll(into, below, n.Value, n.Target)
	case *feel.UnaryTest:
		return n == nil || namesAt(n.Expr, into, below)
	case *feel.UnaryTests:
		return n == nil || namesOfAll(into, below, n.Tests...)
	}
	return false
}

// namesOfAll collects from every node, each at depth, and reports whether all
// could be read. It does not stop at the first that could not: the names that
// can be read are still worth having.
func namesOfAll(into map[string]struct{}, depth int, nodes ...feel.Node) bool {
	readable := true
	for _, node := range nodes {
		readable = namesAt(node, into, depth) && readable
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
