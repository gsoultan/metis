package logic

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/gsoultan/gobpm/internal/pkg/features"
	"github.com/gsoultan/gobpm/server/domains/logic/feel"
	"github.com/rs/zerolog/log"

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
	if !strings.Contains(condition, "=") || isRicherExpression(condition) {
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

	if !features.Enabled(features.JavaScriptConditions) {
		log.Error().
			Str("condition", condition).
			Str("flag", features.EnvName(features.JavaScriptConditions)).
			Msg("A JavaScript condition was refused: this definition needs rewriting in FEEL")
		return false
	}

	// Every use is logged, so migrating away from JavaScript has a worklist
	// rather than an archaeology project. FEEL expresses the same routing
	// without a runtime that can be held past its budget.
	log.Warn().
		Str("condition", condition).
		Msg("A JavaScript condition ran; FEEL is the supported language and this will stop working")

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

// isRicherExpression reports whether a condition is more than the plain
// `key=value` shape this evaluator was written for.
//
// It matters because the guard used to be "contains an equals sign", which
// claimed real expressions too: `status = "GOLD"` was compared against the
// literal text `"GOLD"` — quotes included — and never matched, while
// `amount >= 500` looked up a variable called `amount >` and silently answered
// false. Both now fall through to FEEL, which understands them.
func isRicherExpression(condition string) bool {
	if strings.ContainsAny(condition, `<>!"'()[]`) {
		return true
	}
	lowered := " " + strings.ToLower(condition) + " "
	for _, keyword := range []string{" and ", " or ", " not ", " in ", " between "} {
		if strings.Contains(lowered, keyword) {
			return true
		}
	}
	return false
}

// FEELConditionEvaluator evaluates a condition as FEEL.
//
// It sits last in the chain, which makes it purely additive: every condition
// the earlier evaluators claim keeps its exact previous behaviour, and FEEL
// takes what they decline — which until now returned false without explanation.
// `amount >= 500`, `status = "GOLD"`, `amount > 100 and tier = "GOLD"` and
// `date(dueDate) < today()` all work now and all silently failed before.
type FEELConditionEvaluator struct {
	BaseEvaluator
}

func (e *FEELConditionEvaluator) Evaluate(condition string, vars map[string]any) bool {
	result, err := feel.EvaluateCondition(condition, vars)
	if err != nil {
		// A condition that will not parse or does not yield a boolean is a
		// routing decision nobody can make. Logged rather than swallowed: a
		// gateway that quietly takes the default path is the kind of failure
		// that gets diagnosed weeks later from the wrong end.
		log.Warn().
			Err(err).
			Str("condition", condition).
			Msg("Condition could not be evaluated; treating it as false")
		return e.EvaluateNext(condition, vars)
	}
	return result
}

// GetConditionEvaluatorChain returns the singleton Chain-of-Responsibility for
// condition evaluation.  Evaluators are stateless so a single shared chain is safe
// for concurrent use.  The chain order is:
//
//	EmptyCondition → JSExpression (js: prefix) → Equals (var=value) →
//	SimpleVariable → FEEL
//
// FEEL is last so the integration is additive: everything the earlier
// evaluators claim behaves exactly as before, and FEEL answers what they
// decline — which previously fell off the end as false.
func GetConditionEvaluatorChain() contracts.ConditionEvaluator {
	conditionChainOnce.Do(func() {
		root := &EmptyConditionEvaluator{}
		root.SetNext(&JSExpressionEvaluator{}).
			SetNext(&EqualsEvaluator{}).
			SetNext(&SimpleVariableEvaluator{}).
			SetNext(&FEELConditionEvaluator{})
		conditionChainSingleton = root
	})
	return conditionChainSingleton
}
