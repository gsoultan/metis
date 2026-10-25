package people_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

func (w world) makeKey(t *testing.T, session, name string) (entities.PersonAPIKey, string) {
	t.Helper()
	code, body := w.do(t, http.MethodPost, "/api/v1/people/keys", session, map[string]string{"name": name})
	if code != http.StatusOK {
		t.Fatalf("make the %q key: %d %s", name, code, body)
	}
	var out struct {
		Key   entities.PersonAPIKey `json:"key"`
		Token string                `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.Token, "mpk_") || !strings.HasPrefix(out.Token, out.Key.Prefix) {
		t.Fatalf("key %+v with token %q does not look like one", out.Key, out.Token)
	}
	return out.Key, out.Token
}

// TestAPersonsSystemsWorkTheirTasksWithKeysOfTheirOwn: several named keys,
// each working the person's tasks, each revoked on its own, none of them
// able to make more keys, and all of them stopped when the person is.
func TestAPersonsSystemsWorkTheirTasksWithKeysOfTheirOwn(t *testing.T) {
	w := newWorld(t)
	session := w.signIn(t)
	sheet, sheetKey := w.makeKey(t, session, "approvals spreadsheet")
	_, portalKey := w.makeKey(t, session, "supplier portal")

	if code, body := w.do(t, http.MethodPost, "/api/v1/people/keys", session, map[string]string{"name": "Supplier Portal"}); code != http.StatusBadRequest {
		t.Errorf("a second key of the same name = %d %s, want 400", code, body)
	}

	named := w.startTask(t, w.project, "named", entities.Node{Assignee: "ada"})
	code, body := w.do(t, http.MethodGet, "/api/v1/people/tasks", sheetKey, nil)
	if code != http.StatusOK || !listed(t, body)[named.ID] {
		t.Fatalf("ada's spreadsheet lists her tasks: %d %s", code, body)
	}
	// The key never shows again: the list says which is which by name and prefix.
	_, body = w.do(t, http.MethodGet, "/api/v1/people/keys", session, nil)
	if strings.Contains(body, sheetKey) || !strings.Contains(body, sheet.Prefix) || !strings.Contains(body, "supplier portal") {
		t.Fatalf("the key list %s shows a key, or loses one", body)
	}
	// A key works tasks; it does not make, list or revoke keys.
	if code, body := w.do(t, http.MethodPost, "/api/v1/people/keys", sheetKey, map[string]string{"name": "another"}); code != http.StatusForbidden {
		t.Errorf("a key making a key = %d %s, want 403", code, body)
	}
	if code, body := w.do(t, http.MethodDelete, "/api/v1/people/keys/"+sheet.ID.String(), portalKey, nil); code != http.StatusForbidden {
		t.Errorf("a key revoking a key = %d %s, want 403", code, body)
	}
	// A key is not a User's token anywhere else.
	if code, body := w.do(t, http.MethodGet, "/api/v1/participants?project_id="+w.project.String(), sheetKey, nil); code != http.StatusUnauthorized {
		t.Errorf("a person's key on a User route = %d %s, want 401", code, body)
	}

	// Revoking one leaves the other working.
	if code, body := w.do(t, http.MethodDelete, "/api/v1/people/keys/"+sheet.ID.String(), session, nil); code != http.StatusOK {
		t.Fatalf("revoke the spreadsheet's key: %d %s", code, body)
	}
	if code, body := w.do(t, http.MethodGet, "/api/v1/people/tasks", sheetKey, nil); code != http.StatusUnauthorized {
		t.Errorf("a revoked key = %d %s, want 401", code, body)
	}
	if code, body := w.do(t, http.MethodPost, "/api/v1/people/tasks/"+named.ID.String()+"/complete", portalKey,
		map[string]any{"variables": map[string]any{"approved": true}}); code != http.StatusOK {
		t.Fatalf("the portal completes ada's task with its key: %d %s", code, body)
	}

	// Deactivating ada stops every key she has.
	if _, err := w.conn.Main().Exec(t.Context(), "UPDATE workflow_users SET active = false WHERE id = $1", w.personID(t, "ada")); err != nil {
		t.Fatal(err)
	}
	if code, body := w.do(t, http.MethodGet, "/api/v1/people/tasks", portalKey, nil); code != http.StatusUnauthorized {
		t.Errorf("a deactivated person's key = %d %s, want 401", code, body)
	}
}
