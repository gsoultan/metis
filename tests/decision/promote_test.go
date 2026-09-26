package decision_test

import (
	"net/http"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Any stored version of a decision can be made live again: the way back from a
// bad edit is to put the previous policy back into force, not to type it in
// again as a new version. It is what a process's version history offers, and a
// decision had nothing like it.

func (a *decisionAPI) promote(t *testing.T, role string, version int) (int, map[string]any) {
	t.Helper()
	return a.as(t, role, http.MethodPost, "/api/v1/decisions/versions/promote", map[string]any{
		"project_id": a.project.String(),
		"key":        "credit-band",
		"version":    version,
	})
}

func TestMakingAnOlderVersionLiveChangesWhatAnUnpinnedEvaluationReturns(t *testing.T) {
	api := newDecisionAPI(t)
	first := api.create(t, "HIGH")
	api.save(t, first, "VERY HIGH", false)
	if band, version := api.unpinned(t); band != "VERY HIGH" || version != 2 {
		t.Fatalf("before the rollback: %s from v%d, want VERY HIGH from v2", band, version)
	}

	status, body := api.promote(t, entities.RoleDesigner, 1)
	if status != http.StatusOK {
		t.Fatalf("make v1 live: status %d (%v)", status, body)
	}
	if band, version := api.unpinned(t); band != "HIGH" || version != 1 {
		t.Errorf("after making v1 live an unpinned evaluation answers %s from v%d, want HIGH from v1", band, version)
	}

	// The history says so, and keeps the version rolled back from.
	status, body = api.as(t, entities.RoleUser, http.MethodGet, api.versionsPath(), nil)
	if status != http.StatusOK {
		t.Fatalf("version history: status %d (%v)", status, body)
	}
	versions, _ := body["versions"].([]any)
	var live []float64
	var listed []float64
	for _, entry := range versions {
		row, _ := entry.(map[string]any)
		number, _ := row["version"].(float64)
		listed = append(listed, number)
		if isLive, _ := row["live"].(bool); isLive {
			live = append(live, number)
		}
	}
	if len(listed) != 2 || listed[0] != 2 || listed[1] != 1 {
		t.Errorf("the history lists versions %v, want [2 1], newest first", listed)
	}
	if len(live) != 1 || live[0] != 1 {
		t.Errorf("the history marks %v live, want only v1", live)
	}
}

// A version nobody saved cannot be made live. Accepting it would record a live
// version that is not there, and every step with no version binding would stop
// answering.
func TestMakingAVersionNobodySavedLiveIsRefused(t *testing.T) {
	api := newDecisionAPI(t)
	api.create(t, "HIGH")

	for _, version := range []int{7, 0} {
		status, body := api.promote(t, entities.RoleDesigner, version)
		if status != http.StatusNotFound && status != http.StatusBadRequest {
			t.Errorf("making v%d live: status %d (%v), want a refusal", version, status, body)
		}
	}
	if band, version := api.unpinned(t); band != "HIGH" || version != 1 {
		t.Errorf("after the refusals: %s from v%d, want v1 still live", band, version)
	}
}

// Making a version live changes what every running instance decides from then
// on, which is the same bar as saving one: a designer's, not a login's.
func TestOnlyADesignerCanMakeADecisionVersionLive(t *testing.T) {
	api := newDecisionAPI(t)
	first := api.create(t, "HIGH")
	api.save(t, first, "VERY HIGH", false)

	status, body := api.promote(t, entities.RoleUser, 1)
	if status != http.StatusUnauthorized && status != http.StatusForbidden {
		t.Errorf("an account without the designer role made v1 live: status %d (%v)", status, body)
	}
	if band, version := api.unpinned(t); band != "VERY HIGH" || version != 2 {
		t.Errorf("after the refused promotion: %s from v%d, want v2 still live", band, version)
	}
}
