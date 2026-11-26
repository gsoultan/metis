package grule

import (
	"fmt"
	"strconv"
	"strings"
)

// feelOperators are the GRL infix operators FEEL has, and how FEEL spells
// each. `%` is FEEL's modulo function, and `|` and `&` have no FEEL form.
var feelOperators = map[string]string{
	"&&": "and", "||": "or", "==": "=", "!=": "!=",
	"<": "<", "<=": "<=", ">": ">", ">=": ">=",
	"+": "+", "-": "-", "*": "*", "/": "/",
}

// feelFunctions are Grule's functions FEEL has under another name, with the
// same arguments in the same order.
var feelFunctions = map[string]string{
	"Max": "max", "Min": "min", "Abs": "abs", "Ceil": "ceiling", "Floor": "floor",
	"Mod": "modulo", "Now": "now",
}

// feelMethods are Grule's string methods, as the FEEL function that takes the
// string first and the method's arguments after it.
var feelMethods = map[string]string{
	"Contains": "contains", "HasPrefix": "starts with", "HasSuffix": "ends with",
	"Len": "string length", "ToUpper": "upper case", "ToLower": "lower case",
}

// writeFEEL writes an expression as FEEL. Every path is written as the
// variable it is kept in. An expression FEEL has no exact form for is an
// error saying what, never an approximation.
func writeFEEL(expr Expr) (string, error) {
	switch e := expr.(type) {
	case Literal:
		return feelLiteral(e.Value), nil
	case Path:
		key := fieldKey(e)
		if !usableName(key) {
			return "", fmt.Errorf("the field %s has no name FEEL can use", key)
		}
		return variableName(key), nil
	case Index:
		return "", fmt.Errorf("reading an element of a list or map (`[...]`) is not supported")
	case Unary:
		return writeUnary(e)
	case Binary:
		return writeBinary(e)
	case Call:
		return writeCall(e)
	}
	return "", fmt.Errorf("an expression of type %T is not supported", expr)
}

// feelLiteral writes a constant as FEEL. A string is quoted with the escapes
// FEEL's lexer reads back, so its text can never become part of the
// expression around it.
func feelLiteral(value any) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case string:
		replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`)
		return `"` + replacer.Replace(v) + `"`
	}
	return fmt.Sprintf("%v", value)
}

func writeUnary(e Unary) (string, error) {
	operand, err := writeFEEL(e.Operand)
	if err != nil {
		return "", err
	}
	if e.Op == "!" {
		return "not(" + operand + ")", nil
	}
	return "-(" + operand + ")", nil
}

func writeBinary(e Binary) (string, error) {
	left, err := writeFEEL(e.Left)
	if err != nil {
		return "", err
	}
	right, err := writeFEEL(e.Right)
	if err != nil {
		return "", err
	}
	if e.Op == "%" {
		return "modulo(" + left + ", " + right + ")", nil
	}
	op, ok := feelOperators[e.Op]
	if !ok {
		return "", fmt.Errorf("the operator %s has no FEEL equivalent", e.Op)
	}
	return "(" + left + " " + op + " " + right + ")", nil
}

func writeCall(e Call) (string, error) {
	if e.Receiver != nil {
		return writeMethod(e)
	}
	if e.Name == "IsNil" && len(e.Args) == 1 {
		arg, err := writeFEEL(e.Args[0])
		if err != nil {
			return "", err
		}
		return "(" + arg + " = null)", nil
	}
	name, ok := feelFunctions[e.Name]
	if !ok {
		return "", fmt.Errorf("the function %s() has no FEEL equivalent", e.Name)
	}
	return writeFunction(name, e.Args)
}

func writeMethod(e Call) (string, error) {
	if e.Name == "In" {
		receiver, err := writeFEEL(e.Receiver)
		if err != nil {
			return "", err
		}
		list, err := writeList(e.Args)
		if err != nil {
			return "", err
		}
		return "list contains([" + list + "], " + receiver + ")", nil
	}
	name, ok := feelMethods[e.Name]
	if !ok {
		return "", fmt.Errorf("the method .%s() has no FEEL equivalent", e.Name)
	}
	return writeFunction(name, append([]Expr{e.Receiver}, e.Args...))
}

func writeFunction(name string, args []Expr) (string, error) {
	list, err := writeList(args)
	if err != nil {
		return "", err
	}
	return name + "(" + list + ")", nil
}

func writeList(args []Expr) (string, error) {
	written := make([]string, len(args))
	for i, arg := range args {
		text, err := writeFEEL(arg)
		if err != nil {
			return "", err
		}
		written[i] = text
	}
	return strings.Join(written, ", "), nil
}
