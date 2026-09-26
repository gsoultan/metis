package auth_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// scriptTasksPath is a designer's read: every script body in the organization.
const scriptTasksPath = "/api/v1/definitions/script-tasks"

// Somebody signed in through an identity provider acts as the account linked to
// them, with the roles that account holds in the organization a request is for
// — exactly as a local account does. The token carries no role that counts,
// and the next sign-in leaves the membership that holds the role alone.
func TestAnOIDCAccountsRoleInOneOrganizationActsThereAndNowhereElse(t *testing.T) {
	h := newOIDCHarness(t, organizationClaim)
	acme := h.organization("Acme")
	globex := h.organization("Globex")
	claims := map[string]any{
		"preferred_username": "ada",
		organizationClaim:    []string{acme.String(), globex.String()},
		// Nothing the provider asserts about roles grants any.
		"roles": []string{entities.RoleAdmin, entities.RoleDesigner},
	}
	ada := uuid.MustParse(h.me(h.provider.token(t, "subject-ada", claims)).ID)
	h.holdsIn(ada, acme, entities.RoleDesigner)

	// A later sign-in, as the next request would be.
	token := h.provider.token(t, "subject-ada", claims)
	if status, body := h.get(scriptTasksPath, token, acme.String()); status != http.StatusOK {
		t.Fatalf("Ada, a designer in Acme, asking Acme: got %d (%s), want 200", status, body)
	}
	status, body := h.get(scriptTasksPath, token, globex.String())
	if status != http.StatusForbidden || !strings.Contains(body, entities.RoleDesigner) {
		t.Fatalf("Ada, nobody in Globex, asking Globex: got %d (%s), want a 403 naming the role", status, body)
	}
}

// An organization the provider stops naming is left, and so are the roles the
// account held there: joining it again later starts with none.
func TestAnOrganizationTheClaimStopsNamingTakesItsRolesWithIt(t *testing.T) {
	h := newOIDCHarness(t, organizationClaim)
	acme := h.organization("Acme")
	globex := h.organization("Globex")
	both := map[string]any{"preferred_username": "linus", organizationClaim: []string{acme.String(), globex.String()}}
	linus := uuid.MustParse(h.me(h.provider.token(t, "subject-linus", both)).ID)
	h.holdsIn(linus, globex, entities.RoleDesigner)

	h.me(h.provider.token(t, "subject-linus", map[string]any{
		"preferred_username": "linus", organizationClaim: []string{acme.String()},
	}))
	again := h.provider.token(t, "subject-linus", both)
	if status, body := h.get(scriptTasksPath, again, globex.String()); status != http.StatusForbidden {
		t.Fatalf("back in Globex after leaving it, Linus: got %d (%s), want 403 — the role went with the membership",
			status, body)
	}
}
