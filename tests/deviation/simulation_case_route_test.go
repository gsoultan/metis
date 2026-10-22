package deviation_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

type caseReply struct {
	Case struct {
		ID         string `json:"id"`
		ProcessKey string `json:"process_key"`
		Name       string `json:"name"`
		Scenario   struct {
			Variables map[string]any `json:"variables"`
			Answers   []struct {
				Node string `json:"node"`
				Kind string `json:"kind"`
			} `json:"answers"`
		} `json:"scenario"`
		Expect struct {
			Outcome string         `json:"outcome"`
			EndsAt  string         `json:"ends_at"`
			Vars    map[string]any `json:"variables"`
		} `json:"expect"`
	} `json:"case"`
}

func overLimitCase() map[string]any {
	return map[string]any{
		"name": "Over limit, no manager",
		"scenario": map[string]any{
			"variables": map[string]any{"amount": 4200},
			"answers":   []map[string]any{{"node": "approve", "kind": "person", "actor": "sarah", "after": "PT1H"}},
			"seed":      42,
		},
		"expect": map[string]any{"outcome": "completed", "ends_at": "end", "variables": map[string]any{"approved": true}},
	}
}

// A process's simulation cases are designers' material: saved, read back,
// rewritten and deleted by a designer, refused to everybody else, and
// invisible to another organization.
func TestSimulationCasesAreKeptPerProcessForDesigners(t *testing.T) {
	h := newDeviationHarness(t)
	cases := "/api/v1/projects/" + h.projID.String() + "/simulation-cases/expense-approval"

	designer := h.signIn(t, "designer", entities.RoleDesigner)
	outsider := h.signInElsewhere(t, "outsider-designer", entities.RoleDesigner)

	if status, body := h.do(t, http.MethodGet, "", cases, nil); status != http.StatusUnauthorized {
		t.Fatalf("listing cases signed out answered %d (%s), want 401", status, body)
	}
	for _, role := range []string{entities.RoleUser, entities.RoleOperator} {
		token := h.signIn(t, "not-a-designer-"+role, role)
		if status, body := h.do(t, http.MethodGet, token, cases, nil); status != http.StatusForbidden {
			t.Errorf("a %s listing cases answered %d (%s), want 403", role, status, body)
		}
		if status, body := h.do(t, http.MethodPost, token, cases, overLimitCase()); status != http.StatusForbidden {
			t.Errorf("a %s saving a case answered %d (%s), want 403", role, status, body)
		}
	}
	if status, body := h.do(t, http.MethodPost, outsider, cases, overLimitCase()); status != http.StatusNotFound {
		t.Errorf("another organization's designer saving a case answered %d (%s), want 404", status, body)
	}

	status, body := h.do(t, http.MethodPost, designer, cases, overLimitCase())
	if status != http.StatusOK {
		t.Fatalf("saving a case answered %d (%s), want 200", status, body)
	}
	var saved caseReply
	if err := json.Unmarshal([]byte(body), &saved); err != nil {
		t.Fatalf("read the saved case: %v (%s)", err, body)
	}
	if saved.Case.ID == "" || saved.Case.ProcessKey != "expense-approval" || saved.Case.Expect.EndsAt != "end" ||
		len(saved.Case.Scenario.Answers) != 1 || saved.Case.Scenario.Variables["amount"] != float64(4200) {
		t.Fatalf("the case saved as %+v", saved.Case)
	}
	one := cases + "/" + saved.Case.ID

	status, body = h.do(t, http.MethodGet, designer, cases, nil)
	if status != http.StatusOK || !strings.Contains(body, "Over limit, no manager") {
		t.Errorf("listing the cases answered %d (%s), want the saved case", status, body)
	}
	if status, body := h.do(t, http.MethodGet, outsider, cases, nil); status != http.StatusNotFound {
		t.Errorf("another organization's designer listing the cases answered %d (%s), want 404", status, body)
	}

	renamed := overLimitCase()
	renamed["name"] = "Over limit, manager away"
	if status, body := h.do(t, http.MethodPut, designer, one, renamed); status != http.StatusOK ||
		!strings.Contains(body, "manager away") {
		t.Errorf("rewriting the case answered %d (%s), want 200 with the new name", status, body)
	}
	moved := "/api/v1/projects/" + h.projID.String() + "/simulation-cases/another-process/" + saved.Case.ID
	if status, body := h.do(t, http.MethodPut, designer, moved, renamed); status != http.StatusNotFound {
		t.Errorf("rewriting the case under another process answered %d (%s), want 404: a case is never moved", status, body)
	}
	if status, body := h.do(t, http.MethodDelete, outsider, one, nil); status != http.StatusNotFound {
		t.Errorf("another organization's designer deleting the case answered %d (%s), want 404", status, body)
	}
	if status, body := h.do(t, http.MethodDelete, designer, one, nil); status != http.StatusOK {
		t.Fatalf("deleting the case answered %d (%s), want 200", status, body)
	}
	if status, body := h.do(t, http.MethodGet, designer, cases, nil); status != http.StatusOK || strings.Contains(body, saved.Case.ID) {
		t.Errorf("the deleted case is still listed: %d %s", status, body)
	}
}

// A case that could never be run or checked is refused when it is saved,
// not on every run after it.
func TestASimulationCaseThatCouldNeverBeCheckedIsRefused(t *testing.T) {
	h := newDeviationHarness(t)
	cases := "/api/v1/projects/" + h.projID.String() + "/simulation-cases/expense-approval"
	designer := h.signIn(t, "designer", entities.RoleDesigner)

	tests := []struct {
		name   string
		change func(map[string]any)
	}{
		{"no name", func(c map[string]any) { c["name"] = "   " }},
		{"an outcome no process reaches", func(c map[string]any) {
			c["expect"] = map[string]any{"outcome": "step_budget"}
		}},
		{"an answer of no kind", func(c map[string]any) {
			c["scenario"] = map[string]any{"answers": []map[string]any{{"node": "approve", "kind": "wish"}}}
		}},
		{"a step answered twice", func(c map[string]any) {
			c["scenario"] = map[string]any{"answers": []map[string]any{
				{"node": "approve", "kind": "person"}, {"node": "approve", "kind": "person"},
			}}
		}},
		{"a misspelt field", func(c map[string]any) { c["expects"] = c["expect"] }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body := overLimitCase()
			tc.change(body)
			if status, reply := h.do(t, http.MethodPost, designer, cases, body); status != http.StatusBadRequest {
				t.Errorf("answered %d (%s), want 400", status, reply)
			}
		})
	}

	for i := range entities.MaxSimulationCasesPerProcess {
		if status, reply := h.do(t, http.MethodPost, designer, cases, overLimitCase()); status != http.StatusOK {
			t.Fatalf("saving case %d answered %d (%s), want 200", i+1, status, reply)
		}
	}
	if status, reply := h.do(t, http.MethodPost, designer, cases, overLimitCase()); status != http.StatusBadRequest {
		t.Errorf("a case past the bound answered %d (%s), want 400", status, reply)
	}
}
