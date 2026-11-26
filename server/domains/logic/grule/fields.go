package grule

import (
	"regexp"
	"strings"
)

// fieldKey is how a path is known inside the translator: its parts joined
// with dots, as the rule wrote it.
func fieldKey(p Path) string { return strings.Join(p.Parts, ".") }

// variableName is the process variable a field is kept in: its parts joined
// with underscores, `Applicant.Age` as `Applicant_Age`. Flat, because a
// business rule step writes its answers to top-level variables, and a name
// FEEL reads as one name, because cells and mappings are FEEL.
func variableName(key string) string { return strings.ReplaceAll(key, ".", "_") }

// feelName matches a name FEEL reads as one variable.
var feelName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// feelKeywords are the words FEEL gives a meaning of its own, which a variable
// cannot be called.
var feelKeywords = map[string]bool{
	"and": true, "or": true, "not": true, "true": true, "false": true, "null": true,
	"in": true, "between": true, "instance": true, "of": true, "if": true, "then": true,
	"else": true, "for": true, "some": true, "every": true, "return": true, "satisfies": true,
	"function": true,
}

// usableName reports whether a field's variable can be named in FEEL.
func usableName(key string) bool {
	name := variableName(key)
	return feelName.MatchString(name) && !feelKeywords[strings.ToLower(name)]
}

// pathsIn adds the key of every path an expression reads to into.
func pathsIn(expr Expr, into map[string]bool) {
	switch e := expr.(type) {
	case Path:
		into[fieldKey(e)] = true
	case Index:
		pathsIn(e.Target, into)
		pathsIn(e.Key, into)
	case Call:
		if e.Receiver != nil {
			pathsIn(e.Receiver, into)
		}
		for _, arg := range e.Args {
			pathsIn(arg, into)
		}
	case Unary:
		pathsIn(e.Operand, into)
	case Binary:
		pathsIn(e.Left, into)
		pathsIn(e.Right, into)
	}
}

// conjuncts splits a condition on its top-level `&&`.
func conjuncts(expr Expr) []Expr {
	if b, ok := expr.(Binary); ok && b.Op == "&&" {
		return append(conjuncts(b.Left), conjuncts(b.Right)...)
	}
	return []Expr{expr}
}

// substitute replaces each path that names an already assigned field with the
// value it was given, so `X = 1; Y = X + 1` reads as Grule runs it: Y is 2.
func substitute(expr Expr, assigned map[string]Expr) Expr {
	switch e := expr.(type) {
	case Path:
		if value, ok := assigned[fieldKey(e)]; ok {
			return value
		}
		return e
	case Index:
		return Index{Target: substitute(e.Target, assigned), Key: substitute(e.Key, assigned)}
	case Call:
		call := Call{Name: e.Name}
		if e.Receiver != nil {
			call.Receiver = substitute(e.Receiver, assigned)
		}
		for _, arg := range e.Args {
			call.Args = append(call.Args, substitute(arg, assigned))
		}
		return call
	case Unary:
		return Unary{Op: e.Op, Operand: substitute(e.Operand, assigned)}
	case Binary:
		return Binary{Op: e.Op, Left: substitute(e.Left, assigned), Right: substitute(e.Right, assigned)}
	}
	return expr
}

// usesOperator reports whether an expression applies the infix operator op
// anywhere in it.
func usesOperator(expr Expr, op string) bool {
	switch e := expr.(type) {
	case Binary:
		return e.Op == op || usesOperator(e.Left, op) || usesOperator(e.Right, op)
	case Unary:
		return usesOperator(e.Operand, op)
	case Index:
		return usesOperator(e.Target, op) || usesOperator(e.Key, op)
	case Call:
		if e.Receiver != nil && usesOperator(e.Receiver, op) {
			return true
		}
		for _, arg := range e.Args {
			if usesOperator(arg, op) {
				return true
			}
		}
	}
	return false
}
