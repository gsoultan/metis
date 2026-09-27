package user_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A request acts with the account's global roles and the roles it holds in the
// organization the request is for — the one the tenant resolver settles on —
// and with nothing it holds anywhere else. These go through the production
// chain, because that is where the organization is decided and the role
// checked, in that order.

func TestARoleHeldInOneOrganizationActsThereAndNowhereElse(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("kim", nil, w.acme, w.globex)
	w.holdsIn("kim", w.acme, entities.RoleAdmin)

	if status, body := w.listEnvironments("kim", w.acme, w.acmeProject); status != http.StatusOK {
		t.Fatalf("Kim, an administrator of Acme, reading Acme's environments: got %d (%s), want 200", status, body)
	}
	status, body := w.listEnvironments("kim", w.globex, w.globexProject)
	if status != http.StatusForbidden {
		t.Fatalf("Kim, nobody in Globex, reading Globex's environments: got %d (%s), want 403", status, body)
	}
	if !strings.Contains(body, entities.RoleAdmin) {
		t.Errorf("the refusal %s does not name the role it wants", body)
	}
}

// The organization the role is checked in is the one the data is scoped to: an
// administrator of Acme asking, from Acme, about Globex's project sees nothing
// of it.
func TestARoleHeldInOneOrganizationReachesOnlyThatOrganizationsData(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("kim", nil, w.acme, w.globex)
	w.holdsIn("kim", w.acme, entities.RoleAdmin)

	status, body := w.listEnvironments("kim", w.acme, w.globexProject)
	if status == http.StatusOK && strings.Contains(body, w.globexProject.String()) {
		t.Fatalf("an administrator of Acme reached Globex's project from Acme: %s", body)
	}
}

func TestADesignerInOneOrganizationCannotDeployInAnother(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("dee", nil, w.acme, w.globex)
	w.holdsIn("dee", w.acme, entities.RoleDesigner)

	if status, body := w.deploy("dee", w.acme, w.acmeProject, "acme-intake"); status != http.StatusOK {
		t.Fatalf("Dee, a designer in Acme, deploying into Acme: got %d (%s), want 200", status, body)
	}
	status, body := w.deploy("dee", w.globex, w.globexProject, "globex-intake")
	if status != http.StatusForbidden {
		t.Fatalf("Dee, nobody in Globex, deploying into Globex: got %d (%s), want 403", status, body)
	}
	if !strings.Contains(body, entities.RoleDesigner) {
		t.Errorf("the refusal %s does not name the role it wants", body)
	}
}

// Nothing changes for a role held in every organization: it acts in each of
// them, exactly as every role did before there were any other kind.
func TestAGlobalRoleStillActsInEveryOrganization(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("gus", []string{entities.RoleDesigner}, w.acme, w.globex)

	for name, place := range map[string]struct{ organization, project uuid.UUID }{
		"Acme":   {w.acme, w.acmeProject},
		"Globex": {w.globex, w.globexProject},
	} {
		if status, body := w.deploy("gus", place.organization, place.project, "gus-"+strings.ToLower(name)); status != http.StatusOK {
			t.Errorf("Gus, a designer in every organization, deploying into %s: got %d (%s), want 200", name, status, body)
		}
	}
}

// What every organization shares is not any one organization's to change, so a
// role held in one of them does not reach it — on an installation of one
// organization as much as on one of several. The administrator role held in
// every organization still does, as it always has.
func TestARoleHeldInOneOrganizationDoesNotReachWhatEveryOrganizationShares(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("kim", nil, w.acme)
	w.holdsIn("kim", w.acme, entities.RoleAdmin)
	w.account("root", []string{entities.RoleAdmin}, w.acme)

	shared := []struct {
		name         string
		method, path string
		body         any
	}{
		{"create an organization", http.MethodPost, "/api/v1/organizations", map[string]string{"name": "Initech"}},
		{"list the platform accounts", http.MethodGet, "/api/v1/platform-users", nil},
		{"add a connector template", http.MethodPost, "/api/v1/connectors",
			map[string]any{"connector": map[string]any{"key": "crm.template", "name": "CRM"}}},
	}
	for _, action := range shared {
		status, body := w.call("kim", action.method, action.path, w.acme, action.body)
		if status != http.StatusForbidden {
			t.Errorf("an administrator of Acme alone could %s: got %d (%s), want 403", action.name, status, body)
		}
	}
	if status, body := w.call("root", http.MethodGet, "/api/v1/platform-users", w.acme, nil); status == http.StatusForbidden {
		t.Errorf("an administrator of every organization was refused the platform accounts: %s", body)
	}
}
