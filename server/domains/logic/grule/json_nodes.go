package grule

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// jsonExpr turns one condition object, or a constant standing for one, into
// an expression. A string is GRL text, as Grule reads it; a number or a flag
// is that value.
func jsonExpr(node any, depth int) (Expr, error) {
	if depth > MaxDepth {
		return nil, fmt.Errorf("a condition nests more than %d levels deep", MaxDepth)
	}
	switch v := node.(type) {
	case string:
		return ParseExpression(v)
	case json.Number:
		return ParseExpression(v.String())
	case bool:
		return Literal{Value: v}, nil
	case map[string]any:
		return jsonObject(v, depth)
	case nil:
		return nil, fmt.Errorf("a condition is missing")
	}
	return nil, fmt.Errorf("a %T is not a condition", node)
}

func jsonObject(object map[string]any, depth int) (Expr, error) {
	if len(object) != 1 {
		return nil, fmt.Errorf("a condition object holds exactly one operator, this one holds %d", len(object))
	}
	for name, operand := range object {
		switch name {
		case "const":
			return jsonConst(operand)
		case "obj":
			text, ok := operand.(string)
			if !ok {
				return nil, fmt.Errorf(`"obj" names an object as text`)
			}
			return ParseExpression(text)
		case "call":
			return jsonCall(operand, depth)
		}
		op, known := jsonOperators[name]
		if !known {
			return nil, fmt.Errorf("%q is not a Grule JSON operator", name)
		}
		return jsonBinary(op, operand, depth)
	}
	return nil, fmt.Errorf("an empty condition object")
}

func jsonConst(operand any) (Expr, error) {
	switch v := operand.(type) {
	case string, bool:
		return Literal{Value: v}, nil
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return nil, fmt.Errorf("%s is not a number", v)
		}
		return Literal{Value: f}, nil
	}
	return nil, fmt.Errorf(`"const" holds text, a number or a flag, not a %T`, operand)
}

// jsonBinary folds `{"and": [a, b, c]}` into `a && b && c`, left to right.
func jsonBinary(op string, operand any, depth int) (Expr, error) {
	operands, ok := operand.([]any)
	if !ok || len(operands) < 2 {
		return nil, fmt.Errorf("an operator takes a list of at least two operands")
	}
	// Each operand past the first adds a level to the tree it folds into.
	if depth+len(operands) > MaxDepth {
		return nil, fmt.Errorf("a condition nests more than %d levels deep", MaxDepth)
	}
	expr, err := jsonExpr(operands[0], depth+1)
	if err != nil {
		return nil, err
	}
	for i, next := range operands[1:] {
		right, err := jsonExpr(next, depth+i+1)
		if err != nil {
			return nil, err
		}
		expr = Binary{Op: op, Left: expr, Right: right}
	}
	return expr, nil
}

// jsonCall reads `{"call": ["Name", arg, ...]}`. A dotted name is a method on
// the object before the last dot.
func jsonCall(operand any, depth int) (Expr, error) {
	operands, ok := operand.([]any)
	if !ok || len(operands) == 0 {
		return nil, fmt.Errorf(`"call" takes a list starting with the function's name`)
	}
	name, ok := operands[0].(string)
	if !ok || name == "" {
		return nil, fmt.Errorf(`"call" starts with the function's name as text`)
	}
	call := Call{Name: name}
	if dot := strings.LastIndexByte(name, '.'); dot > 0 {
		receiver, err := ParseExpression(name[:dot])
		if err != nil {
			return nil, err
		}
		call = Call{Receiver: receiver, Name: name[dot+1:]}
	}
	for _, arg := range operands[1:] {
		expr, err := jsonExpr(arg, depth+1)
		if err != nil {
			return nil, err
		}
		call.Args = append(call.Args, expr)
	}
	return call, nil
}

// jsonStatement reads one `then` action: GRL text, `{"set": [target, value]}`
// or `{"call": [...]}`.
func jsonStatement(raw json.RawMessage) (Statement, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var node any
	if err := decoder.Decode(&node); err != nil {
		return nil, err
	}
	if text, isText := node.(string); isText {
		text = strings.TrimSuffix(strings.TrimSpace(text), ";")
		rules, err := ParseGRL("action", "rule A { when true then "+text+"; }")
		if err != nil || len(rules) != 1 || len(rules[0].Then) != 1 {
			return nil, fmt.Errorf("%q is not one GRL action", text)
		}
		return rules[0].Then[0], nil
	}
	object, ok := node.(map[string]any)
	if !ok || len(object) != 1 {
		return nil, fmt.Errorf(`an action is GRL text or an object holding "set" or "call"`)
	}
	if set, isSet := object["set"]; isSet {
		return jsonSet(set)
	}
	if _, isCall := object["call"]; isCall {
		expr, err := jsonObject(object, 0)
		if err != nil {
			return nil, err
		}
		call, isCall := expr.(Call)
		if !isCall {
			return nil, fmt.Errorf(`"call" did not read as a call`)
		}
		return CallStatement{Call: call}, nil
	}
	return nil, fmt.Errorf(`an action is "set" or "call"`)
}

func jsonSet(operand any) (Statement, error) {
	operands, ok := operand.([]any)
	if !ok || len(operands) != 2 {
		return nil, fmt.Errorf(`"set" takes a target and a value`)
	}
	target, err := jsonExpr(operands[0], 1)
	if err != nil {
		return nil, err
	}
	value, err := jsonExpr(operands[1], 1)
	if err != nil {
		return nil, err
	}
	return Assign{Target: target, Op: "=", Value: value}, nil
}
