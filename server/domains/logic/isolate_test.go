package logic

import (
	"testing"

	"github.com/gsoultan/metis/internal/pkg/features"
)

// nestedOrder is a process variable shaped like the business data that
// reaches the engine: a JSON object holding an array of objects.
func nestedOrder() map[string]any {
	return map[string]any{
		"order": map[string]any{
			"status": "open",
			"items":  []any{map[string]any{"sku": "A", "qty": 1.0}},
		},
		"tags": map[string]string{"region": "eu"},
	}
}

func orderStatus(vars map[string]any) any {
	return vars["order"].(map[string]any)["status"]
}

func firstItemQty(vars map[string]any) any {
	items := vars["order"].(map[string]any)["items"].([]any)
	return items[0].(map[string]any)["qty"]
}

// TestAFailingScriptLeavesNestedVariablesUntouched pins the contract RunScript
// documents: a script that fails applies nothing. The shallow clone it used to
// take still handed the runtime the instance's own nested maps, so edits made
// before the throw stayed behind in the instance.
func TestAFailingScriptLeavesNestedVariablesUntouched(t *testing.T) {
	vars := nestedOrder()

	_, err := RunScript(t.Context(), `
		order.status = "shipped";
		order.items[0].qty = 99;
		tags.region = "us";
		throw new Error("boom");
	`, vars)
	if err == nil {
		t.Fatal("the script threw, but RunScript reported success")
	}

	if got := orderStatus(vars); got != "open" {
		t.Errorf("order.status = %v after a failed script, want it untouched", got)
	}
	if got := firstItemQty(vars); got != 1.0 {
		t.Errorf("order.items[0].qty = %v after a failed script, want it untouched", got)
	}
	if got := vars["tags"].(map[string]string)["region"]; got != "eu" {
		t.Errorf("tags.region = %v after a failed script, want it untouched", got)
	}
}

// TestASucceedingScriptReturnsNestedEditsWithoutTouchingItsInput keeps the
// isolation from costing the feature: nested edits still come back in the
// result, they just no longer leak into the map the caller passed in.
func TestASucceedingScriptReturnsNestedEditsWithoutTouchingItsInput(t *testing.T) {
	vars := nestedOrder()

	got, err := RunScript(t.Context(), `order.status = "shipped"; order.items[0].qty = 2;`, vars)
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}

	if s := orderStatus(got); s != "shipped" {
		t.Errorf("result order.status = %v, want shipped", s)
	}
	if q := firstItemQty(got); q != int64(2) && q != 2.0 {
		t.Errorf("result order.items[0].qty = %v, want 2", q)
	}
	if s := orderStatus(vars); s != "open" {
		t.Errorf("input order.status = %v, want the caller's map left alone", s)
	}
}

// TestAJavaScriptConditionCannotRewriteNestedVariables covers gateway
// conditions, which bound the instance's variables straight into the runtime:
// a condition meant only to choose a branch could rewrite business data.
func TestAJavaScriptConditionCannotRewriteNestedVariables(t *testing.T) {
	defer features.OverrideForTest(features.JavaScriptConditions, true)()
	vars := nestedOrder()

	if !GetConditionEvaluatorChain().Evaluate(`js:(order.status = "cancelled", order.items[0].qty = 0, true)`, vars) {
		t.Fatal("the condition should still evaluate to true")
	}

	if got := orderStatus(vars); got != "open" {
		t.Errorf("order.status = %v after a condition ran, want it untouched", got)
	}
	if got := firstItemQty(vars); got != 1.0 {
		t.Errorf("order.items[0].qty = %v after a condition ran, want it untouched", got)
	}
}
