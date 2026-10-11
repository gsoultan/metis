package user_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// A group's roles are held by everyone in it, in the group's organization and
// nowhere else. The group editor has always said so; until now nothing read
// them, so an administrator who gave a group a role granted nothing, and one
// who took a role away through a group took nothing away. These go through the
// production chain, where the caller is resolved and the role checked.

// groupsAdmin is the administrator of Acme who manages its groups in these
// tests.
const groupsAdmin = "ada"

// group creates a group in an organization through the API, as an
// administrator there does, and returns its id. The reply carries no group, so
// the id is chosen here.
func (w *orgRolesWorld) group(organization uuid.UUID, name string, roles ...string) uuid.UUID {
	w.t.Helper()
	id := uuid.Must(uuid.NewV7())
	status, body := w.call(groupsAdmin, http.MethodPost, "/api/v1/organizations/"+organization.String()+"/groups",
		organization, map[string]any{"group": map[string]any{"id": id, "name": name, "roles": roles}})
	if status != http.StatusOK {
		w.t.Fatalf("create the group %s: got %d (%s)", name, status, body)
	}
	return id
}

// join puts an account in a group through the API.
func (w *orgRolesWorld) join(username string, organization, group uuid.UUID) {
	w.t.Helper()
	path := "/api/v1/groups/" + group.String() + "/members/" + w.ids[username].String()
	if status, body := w.call(groupsAdmin, http.MethodPost, path, organization, nil); status != http.StatusOK {
		w.t.Fatalf("put %s in the group: got %d (%s)", username, status, body)
	}
}

func newGroupRolesWorld(t *testing.T) *orgRolesWorld {
	t.Helper()
	w := newOrgRolesWorld(t)
	w.account(groupsAdmin, nil, w.acme)
	w.holdsIn(groupsAdmin, w.acme, entities.RoleAdmin)
	return w
}

func TestAGroupsRoleActsForItsMembersInItsOrganization(t *testing.T) {
	w := newGroupRolesWorld(t)
	w.account("dee", nil, w.acme, w.globex)

	if status, body := w.deploy("dee", w.acme, w.acmeProject, "before-the-group"); status != http.StatusForbidden {
		t.Fatalf("Dee, holding nothing, deploying into Acme: got %d (%s), want 403", status, body)
	}

	designers := w.group(w.acme, "Designers", entities.RoleDesigner)
	w.join("dee", w.acme, designers)

	if status, body := w.deploy("dee", w.acme, w.acmeProject, "acme-intake"); status != http.StatusOK {
		t.Fatalf("Dee, in Acme's designers group, deploying into Acme: got %d (%s), want 200", status, body)
	}
	if status, body := w.deploy("dee", w.globex, w.globexProject, "globex-intake"); status != http.StatusForbidden {
		t.Fatalf("Dee, in Acme's designers group, deploying into Globex: got %d (%s), want 403", status, body)
	}
}

// Taking somebody out of a group takes its roles away on their next request,
// not when the cache of signed-in callers happens to expire.
func TestLeavingAGroupTakesItsRolesAwayAtOnce(t *testing.T) {
	w := newGroupRolesWorld(t)
	w.account("dee", nil, w.acme)
	designers := w.group(w.acme, "Designers", entities.RoleDesigner)
	w.join("dee", w.acme, designers)
	if status, body := w.deploy("dee", w.acme, w.acmeProject, "while-in"); status != http.StatusOK {
		t.Fatalf("Dee, in the group: got %d (%s), want 200", status, body)
	}

	path := "/api/v1/groups/" + designers.String() + "/members/" + w.ids["dee"].String()
	if status, body := w.call(groupsAdmin, http.MethodDelete, path, w.acme, nil); status != http.StatusOK {
		t.Fatalf("take Dee out of the group: got %d (%s)", status, body)
	}
	if status, body := w.deploy("dee", w.acme, w.acmeProject, "after-leaving"); status != http.StatusForbidden {
		t.Fatalf("Dee, out of the group, deploying into Acme: got %d (%s), want 403", status, body)
	}
}

// Changing a group's roles, or deleting it, changes what its members may do on
// their next request.
func TestChangingOrDeletingAGroupChangesItsMembersRolesAtOnce(t *testing.T) {
	w := newGroupRolesWorld(t)
	w.account("dee", nil, w.acme)
	designers := w.group(w.acme, "Designers", entities.RoleDesigner)
	w.join("dee", w.acme, designers)
	if status, body := w.deploy("dee", w.acme, w.acmeProject, "first"); status != http.StatusOK {
		t.Fatalf("Dee, in the group: got %d (%s), want 200", status, body)
	}

	status, body := w.call(groupsAdmin, http.MethodPut, "/api/v1/groups/"+designers.String(), w.acme,
		map[string]any{"group": map[string]any{"name": "Designers", "roles": []string{}}})
	if status != http.StatusOK {
		t.Fatalf("take the group's roles away: got %d (%s)", status, body)
	}
	if status, body := w.deploy("dee", w.acme, w.acmeProject, "after-the-change"); status != http.StatusForbidden {
		t.Fatalf("Dee, in a group that grants nothing now: got %d (%s), want 403", status, body)
	}

	release := w.group(w.acme, "Release", entities.RoleDesigner)
	w.join("dee", w.acme, release)
	if status, body := w.deploy("dee", w.acme, w.acmeProject, "in-release"); status != http.StatusOK {
		t.Fatalf("Dee, in the release group: got %d (%s), want 200", status, body)
	}
	if status, body := w.call(groupsAdmin, http.MethodDelete, "/api/v1/groups/"+release.String(), w.acme, nil); status != http.StatusOK {
		t.Fatalf("delete the release group: got %d (%s)", status, body)
	}
	if status, body := w.deploy("dee", w.acme, w.acmeProject, "after-the-delete"); status != http.StatusForbidden {
		t.Fatalf("Dee, after the group was deleted: got %d (%s), want 403", status, body)
	}
}

// A role a group gives counts in that organization only, so it never reaches
// what every organization shares.
func TestAGroupsRoleDoesNotReachWhatEveryOrganizationShares(t *testing.T) {
	w := newGroupRolesWorld(t)
	w.account("kim", nil, w.acme)
	admins := w.group(w.acme, "Administrators", entities.RoleAdmin)
	w.join("kim", w.acme, admins)

	if status, body := w.listEnvironments("kim", w.acme, w.acmeProject); status != http.StatusOK {
		t.Fatalf("Kim, in Acme's administrators group, reading Acme's environments: got %d (%s), want 200", status, body)
	}
	if status, body := w.call("kim", http.MethodGet, "/api/v1/platform-users", w.acme, nil); status != http.StatusForbidden {
		t.Fatalf("Kim, an administrator of Acme through a group, listing the platform accounts: got %d (%s), want 403", status, body)
	}
}

// A membership left in a group of an organization the account no longer
// belongs to grants nothing there.
func TestAGroupGrantsNothingToSomebodyNoLongerInItsOrganization(t *testing.T) {
	// Every request re-reads its caller, so the membership removed behind the
	// service's back below is seen.
	t.Setenv("METIS_AUTH_CACHE_TTL", "0")
	w := newGroupRolesWorld(t)
	w.account("dee", nil, w.acme)
	designers := w.group(w.acme, "Designers", entities.RoleDesigner)
	w.join("dee", w.acme, designers)

	result := w.db.WithContext(t.Context()).Exec(
		`DELETE FROM user_organizations WHERE user_id = ? AND organization_id = ?`, w.ids["dee"], w.acme)
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("take Dee out of Acme: %v (%d rows)", result.Error, result.RowsAffected)
	}
	roles, err := w.svc.ValidateToken(t.Context(), w.tokens["dee"])
	if err != nil {
		t.Fatalf("resolve Dee: %v", err)
	}
	if held := roles.RolesIn(w.acme); len(held) != 0 {
		t.Fatalf("Dee, out of Acme, still holds %v there through its group", held)
	}
}

// A group is given only the roles the installation has, as an account is in an
// organization: a typo would read as a grant and admit to nothing.
func TestAGroupIsRefusedARoleTheInstallationDoesNotHave(t *testing.T) {
	w := newGroupRolesWorld(t)
	status, body := w.call(groupsAdmin, http.MethodPost, "/api/v1/organizations/"+w.acme.String()+"/groups", w.acme,
		map[string]any{"group": map[string]any{"name": "Typos", "roles": []string{"DESINGER"}}})
	if status != http.StatusBadRequest {
		t.Fatalf("a group with a role nobody has: got %d (%s), want 400", status, body)
	}
}
