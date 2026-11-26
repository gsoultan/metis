package grule

import "github.com/gsoultan/metis/server/domains/logic/feel"

// condition is one `&&`-separated part of a rule's `when`, written for a
// decision table. When it compares one field with a constant it is a cell in
// that field's column (key and cell); otherwise it is FEEL, a boolean
// expression the table tests as a whole (expression).
type condition struct {
	key        string
	cell       string
	expression string
}

// flipped is the operator that says the same with its operands swapped:
// `18 < Age` is `Age > 18`.
var flipped = map[string]string{"==": "==", "!=": "!=", "<": ">", "<=": ">=", ">": "<", ">=": "<="}

// newCondition writes one part of a condition.
func newCondition(expr Expr) (condition, error) {
	if cell, key, ok := simpleCell(expr); ok {
		return condition{key: key, cell: cell}, nil
	}
	feel, err := writeFEEL(expr)
	if err != nil {
		return condition{}, err
	}
	return condition{expression: feel}, nil
}

// simpleCell writes `Field op constant` as the cell under Field's column:
// `> 18`, `"gold"`, `!= null`. A field on its own is `true`, `!Field` is
// `false`.
func simpleCell(expr Expr) (cell, key string, ok bool) {
	switch e := expr.(type) {
	case Path:
		return "true", fieldKey(e), usableName(fieldKey(e))
	case Unary:
		if p, isPath := e.Operand.(Path); isPath && e.Op == "!" {
			return "false", fieldKey(p), usableName(fieldKey(p))
		}
	case Binary:
		op, known := flipped[e.Op]
		if !known {
			return "", "", false
		}
		path, literal, found := pathAndLiteral(e)
		if !found || !usableName(fieldKey(path)) {
			return "", "", false
		}
		if _, leftIsPath := e.Left.(Path); leftIsPath {
			op = e.Op
		}
		return cellFor(op, literal), fieldKey(path), true
	}
	return "", "", false
}

func cellFor(op string, literal Literal) string {
	value := feelLiteral(literal.Value)
	switch op {
	case "==":
		if literal.Value == nil {
			return "= null"
		}
		return value
	case "!=":
		return "!= " + value
	}
	return op + " " + value
}

// check parses what was written with FEEL's own parser. It is written from a
// parsed tree with every string quoted, so this is expected to pass; it is
// here so that a mistake in the writer turns a rule away with its reason
// instead of storing a table whose cells do not parse.
func (c condition) check() error {
	if c.expression != "" {
		_, err := feel.ParseUnaryTests("(" + c.expression + ")")
		return err
	}
	_, err := feel.ParseUnaryTests(c.cell)
	return err
}
