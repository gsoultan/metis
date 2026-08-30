package logic

import (
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/internal/pkg/features"
)

// TestChainIsAdditive is the safety property of the Phase 2.2 integration:
// every condition the legacy evaluators already handled must keep its exact
// previous answer. FEEL sits last in the chain precisely so that adding it
// cannot change a routing decision a live process depends on.
func TestChainIsAdditive(t *testing.T) {
	chain := GetConditionEvaluatorChain()

	vars := map[string]any{
		"approved": true,
		"rejected": false,
		"status":   "approved",
		"amount":   900.0,
		"tier":     "GOLD",
	}

	tests := []struct {
		name      string
		condition string
		want      bool
	}{
		// Empty: always true, the default flow.
		{"empty condition", "", true},

		// SimpleVariable: a bare boolean.
		{"bare true variable", "approved", true},
		{"bare false variable", "rejected", false},

		// Equals: the plain key=value shape, still case-insensitive as before.
		{"plain equality", "status=approved", true},
		{"plain equality misses", "status=rejected", false},
		{"equality is case-insensitive as it always was", "status=APPROVED", true},
		{"equality with spaces", "status = approved", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := chain.Evaluate(tc.condition, vars); got != tc.want {
				t.Errorf("%q = %v, want %v", tc.condition, got, tc.want)
			}
		})
	}
}

// TestFEELAnswersWhatTheChainUsedToDrop covers the other half: conditions that
// previously fell off the end of the chain and returned false without
// explanation. Each of these is a routing decision that silently did not work.
func TestFEELAnswersWhatTheChainUsedToDrop(t *testing.T) {
	chain := GetConditionEvaluatorChain()

	vars := map[string]any{
		"amount":   900.0,
		"tier":     "GOLD",
		"count":    3.0,
		"items":    []any{map[string]any{"price": 10.0}, map[string]any{"price": 5.0}},
		"customer": map[string]any{"country": "GB"},
	}

	tests := []struct {
		name      string
		condition string
		want      bool
	}{
		// Comparison operators: `amount >= 500` used to look up a variable
		// called "amount >" and answer false.
		{"greater than", "amount > 500", true},
		{"greater or equal", "amount >= 900", true},
		{"less than", "amount < 500", false},
		{"not equal", "amount != 100", true},

		// A quoted string used to be compared including its quotes.
		{"quoted equality", `tier = "GOLD"`, true},
		{"quoted equality misses", `tier = "SILVER"`, false},

		// Boolean composition had no representation at all.
		{"and", `amount > 500 and tier = "GOLD"`, true},
		{"and fails", `amount > 5000 and tier = "GOLD"`, false},
		{"or", `amount > 5000 or tier = "GOLD"`, true},
		{"not", "not(amount > 5000)", true},

		// Arithmetic, paths, ranges and functions.
		{"arithmetic", "amount * 2 > 1500", true},
		{"path", `customer.country = "GB"`, true},
		{"range", "count in [1..5]", true},
		{"aggregate", "sum(items.price) = 15", true},

		// The lenient reading that keeps unquoted literals meaning what their
		// author meant, matching the legacy Equals behaviour.
		{"unquoted literal on the right", "tier = GOLD", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := chain.Evaluate(tc.condition, vars); got != tc.want {
				t.Errorf("%q = %v, want %v", tc.condition, got, tc.want)
			}
		})
	}
}

// TestUnparseableConditionIsFalseNotFatal keeps a broken condition from taking
// an instance down. It routes as false — and says so in the log — rather than
// panicking or erroring the whole execution.
func TestUnparseableConditionIsFalseNotFatal(t *testing.T) {
	chain := GetConditionEvaluatorChain()

	for _, condition := range []string{"((((", "amount >", "@@@", "1 +"} {
		if got := chain.Evaluate(condition, map[string]any{"amount": 1.0}); got {
			t.Errorf("the unparseable condition %q routed as true", condition)
		}
	}
}

// TestNonBooleanConditionIsRefused pins that a condition must decide something.
// `amount + 1` is a number, and routing a token on a number is a guess.
func TestNonBooleanConditionIsRefused(t *testing.T) {
	chain := GetConditionEvaluatorChain()
	if chain.Evaluate("amount + 1", map[string]any{"amount": 1.0}) {
		t.Error("a condition yielding a number routed as true")
	}
}

// TestJavaScriptConditionsAreRefusedUnlessEnabled covers the security control:
// `js:` hands authored content to a runtime that cannot be fully bounded, so
// the flag ships off and running JavaScript is the explicit, logged exception.
// Both directions use an override so the test pins behaviour under each flag
// state rather than whatever the process environment happens to say.
func TestJavaScriptConditionsAreRefusedUnlessEnabled(t *testing.T) {
	chain := GetConditionEvaluatorChain()
	vars := map[string]any{"amount": 900.0}

	restore := features.OverrideForTest(features.JavaScriptConditions, false)
	if chain.Evaluate("js:900 > 500", vars) {
		restore()
		t.Fatal("a JavaScript condition ran while the flag refused it")
	}
	// FEEL is unaffected — that is the point of refusing JavaScript.
	if !chain.Evaluate("amount > 500", vars) {
		restore()
		t.Fatal("refusing JavaScript also broke FEEL conditions")
	}
	restore()

	// An installation still migrating can turn it on, and the sandbox is the
	// budget that then applies.
	defer features.OverrideForTest(features.JavaScriptConditions, true)()
	if !chain.Evaluate("js:900 > 500", vars) {
		t.Error("a JavaScript condition did not run with the flag explicitly on")
	}
}

// TestRicherExpressionsBypassEquals pins the narrowed guard on the legacy
// evaluator. It used to claim anything containing an equals sign, which meant
// quoted strings and comparison operators reached it and were mangled.
func TestRicherExpressionsBypassEquals(t *testing.T) {
	claimed := []string{
		`tier = "GOLD"`,
		"amount >= 500",
		"amount != 100",
		`a = 1 and b = 2`,
		"count in [1..5]",
	}
	for _, condition := range claimed {
		t.Run(condition, func(t *testing.T) {
			if !isRicherExpression(condition) {
				t.Errorf("%q would still be claimed by the plain key=value evaluator", condition)
			}
		})
	}

	// The plain shape must still be claimed, or its case-insensitive behaviour
	// would change.
	for _, condition := range []string{"status=approved", "status = approved"} {
		if isRicherExpression(condition) {
			t.Errorf("%q should stay with the legacy evaluator", condition)
		}
	}
}

// TestConditionsCannotBeHeldOpen is the reason this work exists. A FEEL
// condition has no construct that can loop or allocate without bound, so the
// hostile shapes that held the JavaScript runtime for 37 seconds return at once.
func TestConditionsCannotBeHeldOpen(t *testing.T) {
	chain := GetConditionEvaluatorChain()

	hostile := []string{
		strings.Repeat("not(", 60) + "true" + strings.Repeat(")", 60),
		strings.Repeat("1+", 2000) + "1 > 0",
		"[" + strings.Repeat("1,", 2000) + "1] = []",
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, condition := range hostile {
			_ = chain.Evaluate(condition, nil)
		}
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a FEEL condition did not return promptly; the language must be total")
	}
}
