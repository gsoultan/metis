package logic

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/dop251/goja"
	"github.com/gsoultan/gobpm/server/domains/services/contracts"
)

// BaseEvaluator provides common functionality for condition evaluators.
type BaseEvaluator struct {
	next contracts.ConditionEvaluator
}

func (b *BaseEvaluator) SetNext(next contracts.ConditionEvaluator) contracts.ConditionEvaluator {
	b.next = next
	return next
}

func (b *BaseEvaluator) EvaluateNext(condition string, vars map[string]any) bool {
	if b.next != nil {
		return b.next.Evaluate(condition, vars)
	}
	return false
}

// EmptyConditionEvaluator handles empty condition strings.
type EmptyConditionEvaluator struct {
	BaseEvaluator
}

func (e *EmptyConditionEvaluator) Evaluate(condition string, vars map[string]any) bool {
	if condition == "" {
		return true
	}
	return e.EvaluateNext(condition, vars)
}

// SimpleVariableEvaluator handles conditions that check if a variable exists and is true.
type SimpleVariableEvaluator struct {
	BaseEvaluator
}

func (e *SimpleVariableEvaluator) Evaluate(condition string, vars map[string]any) bool {
	if vars == nil {
		return e.EvaluateNext(condition, vars)
	}

	if val, ok := vars[condition]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return e.EvaluateNext(condition, vars)
}

// EqualsEvaluator handles conditions like "var=value".
type EqualsEvaluator struct {
	BaseEvaluator
}

func (e *EqualsEvaluator) Evaluate(condition string, vars map[string]any) bool {
	if !strings.Contains(condition, "=") {
		return e.EvaluateNext(condition, vars)
	}

	parts := strings.Split(condition, "=")
	if len(parts) != 2 {
		return e.EvaluateNext(condition, vars)
	}

	key := strings.TrimSpace(parts[0])
	expected := strings.TrimSpace(parts[1])

	if vars == nil {
		return false
	}

	if val, ok := vars[key]; ok {
		return strings.EqualFold(strings.TrimSpace(interfaceToString(val)), expected)
	}

	return e.EvaluateNext(condition, vars)
}

// interfaceToString converts common Go scalar types to their string representation.
// This is used by EqualsEvaluator to compare process variable values (which may be
// float64 from JSON unmarshaling) against string literals in condition expressions.
func interfaceToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case bool:
		if val {
			return "true"
		}
		return "false"
	case float64:
		// Integers from JSON come in as float64; format without trailing zeros.
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case int:
		return fmt.Sprintf("%d", val)
	case int64:
		return fmt.Sprintf("%d", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// JSExpressionEvaluator handles JavaScript-based conditions prefixed with 'js:'.
type JSExpressionEvaluator struct {
	BaseEvaluator
}

func (e *JSExpressionEvaluator) Evaluate(condition string, vars map[string]any) bool {
	if !strings.HasPrefix(condition, "js:") {
		return e.EvaluateNext(condition, vars)
	}

	script := strings.TrimPrefix(condition, "js:")
	vm := NewSandboxedRuntime()

	for k, v := range vars {
		if err := vm.Set(k, v); err != nil {
			return false
		}
	}

	// Gateway conditions are user-authored and run on the server, so they get
	// the same wall-clock budget and interrupt as script tasks. Without it a
	// condition containing an infinite loop blocks its goroutine forever.
	val, err := RunSandboxed(context.Background(), vm, ScriptTimeout(), func() (goja.Value, error) {
		return vm.RunString(script)
	})
	if err != nil {
		return false
	}

	return val.ToBoolean()
}

// conditionChainOnce ensures the singleton evaluator chain is built exactly once.
var (
	conditionChainOnce      sync.Once
	conditionChainSingleton contracts.ConditionEvaluator
)

// GetConditionEvaluatorChain returns the singleton Chain-of-Responsibility for
// condition evaluation.  Evaluators are stateless so a single shared chain is safe
// for concurrent use.  The chain order is:
//
//	EmptyCondition → JSExpression (js: prefix) → Equals (var=value) → SimpleVariable
func GetConditionEvaluatorChain() contracts.ConditionEvaluator {
	conditionChainOnce.Do(func() {
		root := &EmptyConditionEvaluator{}
		root.SetNext(&JSExpressionEvaluator{}).
			SetNext(&EqualsEvaluator{}).
			SetNext(&SimpleVariableEvaluator{})
		conditionChainSingleton = root
	})
	return conditionChainSingleton
}
