package decision_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

const discountRules = `
rule Gold "Gold customers get twenty off" salience 30 {
    when Customer.Tier == "gold" && Order.Discount == 0
    then Order.Discount = Order.Total * 0.2;
}
rule Silver salience 20 {
    when Customer.Tier == "silver" && Order.Discount == 0
    then Order.Discount = 10;
}
rule Audit { when Order.Total > 1000 then Order.Audit = Customer.Name.Trim(); Retract("Audit"); }
`

const freeShippingJSON = `[{"name": "FreeShipping", "salience": 5,
  "when": {"gt": ["Order.Total", 100]},
  "then": [{"set": ["Order.Shipping", 0]}, {"call": ["Retract", {"const": "FreeShipping"}]}]}]`

func (a *decisionAPI) importGrule(t *testing.T, role string, extra map[string]any) (int, map[string]any) {
	t.Helper()
	body := map[string]any{
		"project_id": a.project.String(),
		"key":        "pricing",
		"name":       "Pricing",
		"files": []map[string]string{
			{"name": "discount.grl", "content": discountRules},
			{"name": "shipping.json", "content": freeShippingJSON},
		},
	}
	for k, v := range extra {
		body[k] = v
	}
	return a.as(t, role, http.MethodPost, "/api/v1/decisions/import/grule", body)
}

// TestImportingGruleRulesMakesTheDecisionsAndTheProcess: a preview stores
// nothing; an import stores the tables and deploys a process that, started
// with Grule's facts, decides what Grule decides.
func TestImportingGruleRulesMakesTheDecisionsAndTheProcess(t *testing.T) {
	api := newDecisionAPI(t)

	status, body := api.importGrule(t, entities.RoleDesigner, map[string]any{"preview": true})
	if status != http.StatusOK {
		t.Fatalf("preview: status %d (%v)", status, body)
	}
	preview := body["import"].(map[string]any)
	if preview["stored"] == true || len(preview["decisions"].([]any)) != 2 {
		t.Fatalf("preview: %v", preview)
	}
	for _, r := range preview["rules"].([]any) {
		rule := r.(map[string]any)
		if rule["name"] == "Audit" && !strings.Contains(rule["reason"].(string), ".Trim()") {
			t.Errorf("Audit was not reported as using a method FEEL lacks: %v", rule)
		}
	}
	if status, list := api.as(t, entities.RoleDesigner, http.MethodGet, "/api/v1/decisions?project_id="+api.project.String(), nil); status != http.StatusOK || len(list["decisions"].([]any)) != 0 {
		t.Fatalf("a preview stored decisions: %d %v", status, list)
	}

	status, body = api.importGrule(t, entities.RoleDesigner, map[string]any{"deploy": true})
	if status != http.StatusOK {
		t.Fatalf("import: status %d (%v)", status, body)
	}
	imported := body["import"].(map[string]any)
	if imported["stored"] != true || imported["process_id"] == "" {
		t.Fatalf("import: %v", imported)
	}

	variables := api.runPricing(t, map[string]any{
		"Customer": map[string]any{"Tier": "gold"},
		"Order":    map[string]any{"Total": 250, "Discount": 0, "Shipping": 9},
	})
	if variables["Order_Discount"] != 50.0 || variables["Order_Shipping"] != 0.0 {
		t.Errorf("the process decided %v; Grule decides a discount of 50 and free shipping", variables)
	}

	// The same key again is refused rather than filed as a new version of a
	// policy other processes may consult.
	if status, body := api.importGrule(t, entities.RoleDesigner, nil); status != http.StatusBadRequest || !strings.Contains(body["error"].(string), "already used") {
		t.Errorf("a second import under the same key: %d %v", status, body)
	}
}

func TestImportingGruleRulesNeedsADesigner(t *testing.T) {
	api := newDecisionAPI(t)
	if status, _ := api.importGrule(t, entities.RoleUser, map[string]any{"preview": true}); status != http.StatusForbidden {
		t.Errorf("a user imported rules: status %d", status)
	}
}

func TestABrokenRuleFileIsRefusedWithItsLine(t *testing.T) {
	api := newDecisionAPI(t)
	status, body := api.as(t, entities.RoleDesigner, http.MethodPost, "/api/v1/decisions/import/grule", map[string]any{
		"project_id": api.project.String(), "key": "broken", "preview": true,
		"files": []map[string]string{{"name": "broken.grl", "content": "rule A {\n when X > \n then Y = 1; }"}},
	})
	if status != http.StatusBadRequest || !strings.Contains(body["error"].(string), "broken.grl: rule A: line 3: expected a value, found `then`") {
		t.Errorf("status %d, %v", status, body)
	}
}

// runPricing starts the imported process and returns its variables once it
// has finished.
func (a *decisionAPI) runPricing(t *testing.T, variables map[string]any) map[string]any {
	t.Helper()
	status, body := a.as(t, entities.RoleDesigner, http.MethodPost, "/api/v1/process/start", map[string]any{
		"project_id": a.project.String(), "definition_key": "pricing", "variables": variables,
	})
	id, _ := body["instance_id"].(string)
	if status != http.StatusOK || id == "" {
		t.Fatalf("start: status %d (%v)", status, body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		status, body = a.as(t, entities.RoleDesigner, http.MethodGet, "/api/v1/instances/"+id, nil)
		instance, _ := body["instance"].(map[string]any)
		if status == http.StatusOK && instance["status"] == "completed" {
			vars, _ := instance["variables"].(map[string]any)
			return vars
		}
		if time.Now().After(deadline) {
			t.Fatalf("the instance did not complete: %d %v", status, body)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
