package grule_test

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/gsoultan/metis/server/domains/logic/grule"
)

// runGrule is a small model of Grule's engine, for the rules these tests use:
// on each cycle every rule not retracted is tested against the facts, the
// matching one of highest salience (then the first written) fires, and that
// repeats until none matches. Facts are maps of field to value.
func runGrule(rules []grule.Rule, facts map[string]map[string]any) (map[string]map[string]any, error) {
	retracted := map[string]bool{}
	for range 500 {
		var chosen *grule.Rule
		for i := range rules {
			r := &rules[i]
			if retracted[r.Name] {
				continue
			}
			ok, err := truth(r.When, facts)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", r.Name, err)
			}
			if ok && (chosen == nil || r.Salience > chosen.Salience) {
				chosen = r
			}
		}
		if chosen == nil {
			return facts, nil
		}
		if err := fire(*chosen, facts, retracted); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("the rules never stop firing")
}

func fire(r grule.Rule, facts map[string]map[string]any, retracted map[string]bool) error {
	for _, s := range r.Then {
		switch s := s.(type) {
		case grule.CallStatement:
			if s.Call.Name == "Retract" {
				retracted[r.Name] = true
			}
		case grule.Assign:
			value, err := eval(s.Value, facts)
			if err != nil {
				return err
			}
			if s.Op != "=" {
				current, _ := eval(s.Target, facts)
				value = arithmetic(strings.TrimSuffix(s.Op, "="), current, value)
			}
			p := s.Target.(grule.Path)
			facts[p.Parts[0]][p.Parts[1]] = value
		}
	}
	return nil
}

func truth(e grule.Expr, facts map[string]map[string]any) (bool, error) {
	v, err := eval(e, facts)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%v is not true or false", v)
	}
	return b, nil
}

func eval(e grule.Expr, facts map[string]map[string]any) (any, error) {
	switch e := e.(type) {
	case grule.Literal:
		return e.Value, nil
	case grule.Path:
		return facts[e.Parts[0]][e.Parts[1]], nil
	case grule.Unary:
		v, err := eval(e.Operand, facts)
		if err != nil {
			return nil, err
		}
		if e.Op == "!" {
			return !v.(bool), nil
		}
		return -v.(float64), nil
	case grule.Call:
		return call(e, facts)
	case grule.Binary:
		return binary(e, facts)
	}
	return nil, fmt.Errorf("cannot evaluate %T", e)
}

func call(e grule.Call, facts map[string]map[string]any) (any, error) {
	args := make([]any, len(e.Args))
	for i, a := range e.Args {
		v, err := eval(a, facts)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	if e.Receiver == nil {
		if e.Name == "Max" {
			return math.Max(args[0].(float64), args[1].(float64)), nil
		}
		return nil, fmt.Errorf("no function %s", e.Name)
	}
	receiver, err := eval(e.Receiver, facts)
	if err != nil {
		return nil, err
	}
	s := receiver.(string)
	switch e.Name {
	case "Contains":
		return strings.Contains(s, args[0].(string)), nil
	case "In":
		return slices.Contains(args, any(s)), nil
	case "ToUpper":
		return strings.ToUpper(s), nil
	}
	return nil, fmt.Errorf("no method %s", e.Name)
}

func binary(e grule.Binary, facts map[string]map[string]any) (any, error) {
	l, err := eval(e.Left, facts)
	if err != nil {
		return nil, err
	}
	if e.Op == "&&" && l == false {
		return false, nil
	}
	if e.Op == "||" && l == true {
		return true, nil
	}
	r, err := eval(e.Right, facts)
	if err != nil {
		return nil, err
	}
	switch e.Op {
	case "&&", "||":
		return r, nil
	case "==":
		return l == r, nil
	case "!=":
		return l != r, nil
	case "<":
		return l.(float64) < r.(float64), nil
	case "<=":
		return l.(float64) <= r.(float64), nil
	case ">":
		return l.(float64) > r.(float64), nil
	case ">=":
		return l.(float64) >= r.(float64), nil
	}
	return arithmetic(e.Op, l, r), nil
}

func arithmetic(op string, l, r any) any {
	if ls, ok := l.(string); ok {
		return ls + r.(string)
	}
	a, b := l.(float64), r.(float64)
	switch op {
	case "+":
		return a + b
	case "-":
		return a - b
	case "*":
		return a * b
	}
	return a / b
}
