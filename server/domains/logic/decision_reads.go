package logic

import (
	"slices"
	"strings"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/logic/feel"
)

// DecisionTableReads reports what one decision table reads from the variables
// it is evaluated with: the names, the keys of the decisions it requires, and
// whether all of it could be told. Both lists are sorted, each entry once.
//
// It is ReferencedNames for a table, and has the same rule: what cannot be
// read makes the table not analysable, never a table that reads nothing. The
// names that could be read are returned either way.
//
// A table reads in three ways, and this follows the evaluator in each
// (DecisionTableEvaluatorImpl.ruleMatches, feel.EvaluateUnaryTests,
// decisionService.evaluateRecursive):
//
//   - A column. The evaluator looks the column's text up as one variable's
//     name and never parses it, so the text is the name read, whatever it
//     looks like. Where it also parses as an expression the names in that are
//     added: a column written `applicant.income` is meant to read applicant,
//     and a name too many is the safe mistake.
//   - A cell. Every variable is in scope by name, so `> credit_limit` reads a
//     variable no column names. A lone word is text unless it names a column;
//     `_input` is the cell's own column and never a variable. A cell past the
//     last column is never tested, and is not read here.
//   - A decision it requires, which is evaluated with the same variables.
//     Those are returned as keys: which version of each is in force, and what
//     that version reads, is the caller's to find.
//
// The answer of a required decision is put among the variables under its key
// before the table is evaluated, so a column of that name reads the answer
// and not the process's variable. It is listed as read all the same.
func DecisionTableReads(table entities.DecisionDefinition) (names, requires []string, analysable bool) {
	read := map[string]struct{}{}
	analysable = true

	columns := make([]string, len(table.Inputs))
	for i, input := range table.Inputs {
		columns[i] = input.Expression
		if input.Expression == "" {
			continue
		}
		read[input.Expression] = struct{}{}
		if parsed, readable := feelNames(input.Expression, read); parsed && !readable {
			analysable = false
		}
	}

	for _, rule := range table.Rules {
		for i, cell := range rule.Inputs {
			if i >= len(columns) {
				break
			}
			analysable = cellNames(cell, columns, read) && analysable
		}
	}

	return sortedNames(read), requiredDecisions(table), analysable
}

// cellNames adds the variables one cell of a decision table reads to into,
// and reports whether the cell could be read.
func cellNames(cell string, columns []string, into map[string]struct{}) bool {
	tree, err := feel.ParseUnaryTests(cell)
	if err != nil {
		return false
	}
	tests, isList := tree.(*feel.UnaryTests)
	if !isList || tests == nil {
		return false
	}

	// Collected apart from the table's names so that `_input` can be taken
	// out of this cell's without touching a column that is really named that.
	read := map[string]struct{}{}
	readable := true
	for _, node := range tests.Tests {
		test, isTest := node.(*feel.UnaryTest)
		if !isTest || test == nil {
			readable = false
			continue
		}
		if isCellText(test, columns) {
			continue
		}
		readable = collectNames(test.Expr, read) && readable
	}
	delete(read, feel.InputName)
	for name := range read {
		into[name] = struct{}{}
	}
	return readable
}

// isCellText reports whether a test is a lone word the evaluator reads as
// text: no operator, and a name that is neither `_input` nor one of the
// table's columns (evaluator.evalUnaryTest).
func isCellText(test *feel.UnaryTest, columns []string) bool {
	if test.Op != "" {
		return false
	}
	word, isName := test.Expr.(*feel.Name)
	if !isName || word == nil {
		return false
	}
	return word.Text != feel.InputName && !slices.Contains(columns, word.Text)
}

// requiredDecisions is the keys a table requires, sorted, each once, nil when
// it requires none. A blank key names no decision: evaluating it fails, which
// is an incident and not a default.
func requiredDecisions(table entities.DecisionDefinition) []string {
	keys := map[string]struct{}{}
	for _, key := range table.RequiredDecisions {
		if key != "" {
			keys[key] = struct{}{}
		}
	}
	return sortedNames(keys)
}

// MappingSourceNames reports which variables a step's mapping of names to
// values reads — a business rule task's input mapping — and whether that
// could be told. The names are sorted, each once.
//
// It follows mapping.Resolve. A source that is not text is a constant. One
// that is text is first looked up as a variable of exactly that name, so the
// text is always a name read; failing that it is evaluated as FEEL, so the
// names in it are read too. A source FEEL cannot parse leaves its target
// unset with a line in the log: it is reported as not analysable, with its
// text still among the names, because a mapping nobody can read is not one to
// vouch for.
func MappingSourceNames(mapping map[string]any) (names []string, analysable bool) {
	read := map[string]struct{}{}
	analysable = true
	for _, source := range mapping {
		text, isText := source.(string)
		if !isText || strings.TrimSpace(text) == "" {
			continue
		}
		read[text] = struct{}{}
		if parsed, readable := feelNames(text, read); !parsed || !readable {
			analysable = false
		}
	}
	return sortedNames(read), analysable
}
