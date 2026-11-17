package auth_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// keyReply is what making an integration API key answers.
type keyReply struct {
	ID    string   `json:"id"`
	Key   string   `json:"key"`
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
}

// makeKey has the administrator behind session make a key for org.
func (h *apiHarness) makeKey(session string, org uuid.UUID, name string, roles ...string) keyReply {
	h.t.Helper()
	status, raw := h.do(http.MethodPost, "/api/v1/integration-keys", session, org.String(),
		map[string]any{"name": name, "roles": roles})
	if status != http.StatusOK {
		h.t.Fatalf("make the key %s: status %d (%s)", name, status, raw)
	}
	var reply keyReply
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		h.t.Fatalf("decode the key: %v (%s)", err, raw)
	}
	if !strings.HasPrefix(reply.Key, "mik_") {
		h.t.Fatalf("the key %q does not start mik_", reply.Key)
	}
	return reply
}

// A key another system holds calls the API as its organization, with no
// password and no code, on a server where every staff sign-in needs a code.
func TestAnIntegrationKeyCallsTheAPIWithoutACode(t *testing.T) {
	h := newTwoFactorHarness(t)
	acme := h.organization("Acme")
	other := h.organization("Other")
	h.admin("grace", acme)
	_, session, _ := h.enrol("grace")

	key := h.makeKey(session, acme, "billing system", entities.RoleOperator)

	if got := h.projects(key.Key, ""); len(got) != 1 || got[0] != "Acme Project" {
		t.Errorf("the key sees projects %v; want Acme's alone", got)
	}
	if status, raw := h.get("/api/v1/projects", key.Key, other.String()); status != http.StatusForbidden {
		t.Errorf("the key asked for another organization: status %d (%s); want 403", status, raw)
	}
}

// A key is not an account: it cannot read or change one, make more keys, or
// reach the installation's settings, whatever roles it has.
func TestAnIntegrationKeyIsRefusedWhereOnlyASessionMayGo(t *testing.T) {
	h := newTwoFactorHarness(t)
	acme := h.organization("Acme")
	h.admin("grace", acme)
	_, session, _ := h.enrol("grace")
	key := h.makeKey(session, acme, "ci", entities.RoleDesigner, entities.RoleReleaser)

	for _, call := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/users/me"},
		{http.MethodGet, "/api/v1/users/me/two-factor"},
		{http.MethodPost, "/api/v1/users/me/password"},
		{http.MethodGet, "/api/v1/users"},
		{http.MethodGet, "/api/v1/integration-keys"},
		{http.MethodPost, "/api/v1/integration-keys"},
		{http.MethodGet, "/api/v1/settings"},
		{http.MethodGet, "/api/v1/organizations"},
	} {
		status, raw := h.do(call.method, call.path, key.Key, "", map[string]any{"name": "more", "roles": []string{"USER"}})
		if status != http.StatusForbidden {
			t.Errorf("%s %s with a key: status %d (%s); want 403", call.method, call.path, status, raw)
		}
	}
}

// A key can be given the roles that do the organization's work, never the
// administrator's.
func TestAnIntegrationKeyIsNeverAnAdministrator(t *testing.T) {
	h := newTwoFactorHarness(t)
	acme := h.organization("Acme")
	h.admin("grace", acme)
	_, session, _ := h.enrol("grace")

	for _, roles := range [][]string{{entities.RoleAdmin}, {"admin"}, {}, {"NOBODY"}} {
		status, raw := h.do(http.MethodPost, "/api/v1/integration-keys", session, acme.String(),
			map[string]any{"name": "too much", "roles": roles})
		if status != http.StatusBadRequest {
			t.Errorf("a key with roles %v: status %d (%s); want 400", roles, status, raw)
		}
	}
	past := time.Now().Add(-time.Hour)
	status, raw := h.do(http.MethodPost, "/api/v1/integration-keys", session, acme.String(),
		map[string]any{"name": "late", "roles": []string{"USER"}, "expires_at": past})
	if status != http.StatusBadRequest {
		t.Errorf("a key that has already expired: status %d (%s); want 400", status, raw)
	}
}

// Only an administrator of the organization manages its keys, and a revoked
// key stops working at once.
func TestOnlyTheOrganizationsAdministratorsManageItsKeys(t *testing.T) {
	h := newTwoFactorHarness(t)
	acme := h.organization("Acme")
	other := h.organization("Other")
	h.admin("grace", acme)
	_, grace, _ := h.enrol("grace")
	key := h.makeKey(grace, acme, "warehouse", entities.RoleUser)

	h.localAccount("dan", "dan@example.test", acme)
	_, dan, _ := h.enrol("dan")
	if status, raw := h.do(http.MethodPost, "/api/v1/integration-keys", dan, acme.String(),
		map[string]any{"name": "mine", "roles": []string{"USER"}}); status != http.StatusForbidden {
		t.Errorf("a designer made a key: status %d (%s); want 403", status, raw)
	}

	otherAdmin := entities.User{
		ID:            uuid.Must(uuid.NewV7()),
		Username:      "olga",
		Roles:         []string{},
		Organizations: []*entities.Organization{{ID: other}},
	}
	if err := h.svc.CreateUser(inOrganization(t.Context(), other), otherAdmin, localPassword); err != nil {
		t.Fatalf("create olga: %v", err)
	}
	h.holdsIn(otherAdmin.ID, other, entities.RoleAdmin)
	_, olga, _ := h.enrol("olga")
	if status, raw := h.get("/api/v1/integration-keys", olga, other.String()); status != http.StatusOK || strings.Contains(raw, key.ID) {
		t.Errorf("another organization's administrator listed: status %d (%s); want 200 without Acme's key", status, raw)
	}
	if status, raw := h.do(http.MethodDelete, "/api/v1/integration-keys/"+key.ID, olga, other.String(), nil); status != http.StatusNotFound {
		t.Errorf("another organization's administrator revoked Acme's key: status %d (%s); want 404", status, raw)
	}

	if status, raw := h.do(http.MethodDelete, "/api/v1/integration-keys/"+key.ID, grace, acme.String(), nil); status != http.StatusOK {
		t.Fatalf("revoke: status %d (%s)", status, raw)
	}
	if status, raw := h.get("/api/v1/projects", key.Key, ""); status != http.StatusUnauthorized {
		t.Errorf("a revoked key: status %d (%s); want 401", status, raw)
	}
	status, raw := h.get("/api/v1/integration-keys", grace, acme.String())
	if status != http.StatusOK || !strings.Contains(raw, `"revoked_at"`) || strings.Contains(raw, key.Key) {
		t.Errorf("the list after revoking: status %d (%s); want the key marked revoked and never the key itself", status, raw)
	}
}
