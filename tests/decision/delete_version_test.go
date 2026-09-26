package decision_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Deleting removes one stored version, as it always did — the id names a
// version. What it must not do is remove the live one while others remain:
// every step with no version binding would then have nothing to evaluate, and
// the versions still stored would not be the answer to anything until
// somebody noticed. It is refused, with what to do instead. The only version
// of a decision is the exception: deleting it is deleting the decision.

func (a *decisionAPI) remove(t *testing.T, id string) (int, map[string]any) {
	t.Helper()
	return a.as(t, entities.RoleDesigner, http.MethodDelete, "/api/v1/decisions/"+id, nil)
}

func TestTheLiveVersionCannotBeDeletedWhileOtherVersionsRemain(t *testing.T) {
	api := newDecisionAPI(t)
	first := api.create(t, "HIGH")
	second, _ := api.save(t, first, "VERY HIGH", false)["id"].(string)

	status, body := api.remove(t, second)
	message, _ := body["error"].(string)
	if status != http.StatusBadRequest && status != http.StatusConflict {
		t.Fatalf("deleting the live v2 while v1 remains: status %d (%v), want it refused", status, body)
	}
	if !strings.Contains(message, "make another version live") {
		t.Errorf("the refusal says %q, which does not say what to do instead", message)
	}
	if !strings.Contains(message, "and 1 other version remains;") {
		t.Errorf("the refusal says %q; it should say how many other versions remain, in a sentence", message)
	}
	if band, version := api.unpinned(t); band != "VERY HIGH" || version != 2 {
		t.Errorf("after the refusal: %s from v%d, want v2 still live", band, version)
	}

	// A version that is not live goes, and the live one keeps answering.
	if status, body := api.remove(t, first); status != http.StatusOK {
		t.Fatalf("deleting v1, which is not live: status %d (%v)", status, body)
	}
	if band, version := api.unpinned(t); band != "VERY HIGH" || version != 2 {
		t.Errorf("after deleting v1: %s from v%d, want v2 still live", band, version)
	}

	// With nothing else left, deleting the live version deletes the decision.
	if status, body := api.remove(t, second); status != http.StatusOK {
		t.Fatalf("deleting the only version: status %d (%v)", status, body)
	}
	if rows := api.list(t, ""); len(rows) != 0 {
		t.Errorf("after deleting its only version the list still holds %+v", rows)
	}
}
