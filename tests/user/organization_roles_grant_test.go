package user_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// An organization's administrators grant and revoke roles in their own
// organization. A role held in every organization is the platform's: granting
// one, taking one away, or deleting an account that holds one takes a platform
// administrator — one the operator names in METIS_PLATFORM_ADMINS where there
// is more than one organization, and any administrator of every organization
// where there is one.

// platformSetting is what the refusals name.
const platformSetting = "METIS_PLATFORM_ADMINS"

// grantHere replaces the roles target holds in the organization the request is
// for, as the role matrix and the Users page do.
func (w *orgRolesWorld) grantHere(as, target string, organization uuid.UUID, roles ...string) (int, string) {
	w.t.Helper()
	if roles == nil {
		roles = []string{}
	}
	return w.call(as, http.MethodPut, "/api/v1/users/"+w.ids[target].String()+"/organization-roles", organization,
		map[string]any{"roles": roles})
}

// grantEverywhere replaces target's global roles through the account update,
// sending its names and email as they were seeded.
func (w *orgRolesWorld) grantEverywhere(as, target string, organization uuid.UUID, roles ...string) (int, string) {
	w.t.Helper()
	if roles == nil {
		roles = []string{}
	}
	return w.call(as, http.MethodPut, "/api/v1/users/"+w.ids[target].String(), organization, map[string]any{
		"user": map[string]any{"full_name": target, "email": target + "@example.com", "roles": roles},
	})
}

// listedAccount is an account as an organization's list shows it.
type listedAccount struct {
	Username          string   `json:"username"`
	Roles             []string `json:"roles"`
	OrganizationRoles []string `json:"organization_roles"`
}

// listed is the organization's list of accounts, as somebody in it reads it.
func (w *orgRolesWorld) listed(as string, organization uuid.UUID) map[string]listedAccount {
	w.t.Helper()
	status, body := w.call(as, http.MethodGet, "/api/v1/organizations/"+organization.String()+"/users", organization, nil)
	if status != http.StatusOK {
		w.t.Fatalf("list the accounts as %s: got %d (%s)", as, status, body)
	}
	var reply struct {
		Users []listedAccount `json:"users"`
	}
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		w.t.Fatalf("decode the accounts: %v (%s)", err, body)
	}
	out := map[string]listedAccount{}
	for _, account := range reply.Users {
		out[account.Username] = account
	}
	return out
}

func refusedNaming(t *testing.T, status int, body string, words ...string) {
	t.Helper()
	if status != http.StatusForbidden {
		t.Fatalf("got %d (%s), want 403", status, body)
	}
	for _, word := range words {
		if !strings.Contains(body, word) {
			t.Errorf("the refusal %s does not say %q", body, word)
		}
	}
}

func TestAnOrganizationsAdministratorGrantsARoleInTheirOwnOrganization(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("ana", nil, w.acme)
	w.holdsIn("ana", w.acme, entities.RoleAdmin)
	w.account("kim", nil, w.acme, w.globex)

	if status, body := w.grantHere("ana", "kim", w.acme, entities.RoleAdmin); status != http.StatusOK {
		t.Fatalf("Ana, an administrator of Acme, making Kim one: got %d (%s), want 200", status, body)
	}
	if status, body := w.listEnvironments("kim", w.acme, w.acmeProject); status != http.StatusOK {
		t.Fatalf("Kim, now an administrator of Acme, reading Acme's environments: got %d (%s), want 200", status, body)
	}
	status, body := w.listEnvironments("kim", w.globex, w.globexProject)
	refusedNaming(t, status, body, entities.RoleAdmin)

	// Acme's list says so; Globex's, which Kim is also in, says nothing of it.
	if got := w.listed("ana", w.acme)["kim"]; !slices.Equal(got.OrganizationRoles, []string{entities.RoleAdmin}) || len(got.Roles) != 0 {
		t.Errorf("Acme lists Kim with roles %v here and %v everywhere, want [ADMIN] here and none everywhere",
			got.OrganizationRoles, got.Roles)
	}
	w.account("bea", []string{entities.RoleAdmin}, w.globex)
	if got := w.listed("bea", w.globex)["kim"]; len(got.OrganizationRoles) != 0 {
		t.Errorf("Globex's list shows the roles Kim holds in Acme: %v", got.OrganizationRoles)
	}
}

// The grant the Users page used to make — the account's own roles, through
// PUT /api/v1/users/{id} — acted in every organization the account was in.
// Made by an administrator of both organizations, it made Kim an
// administrator of Globex as well. It is the platform's to make now.
func TestAnOrganizationsAdministratorCannotGrantARoleInEveryOrganization(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("sue", []string{entities.RoleAdmin}, w.acme, w.globex)
	w.account("kim", nil, w.acme, w.globex)

	status, body := w.grantEverywhere("sue", "kim", w.acme, entities.RoleAdmin)
	if status != http.StatusForbidden || !strings.Contains(body, platformSetting) || !strings.Contains(body, w.ids["sue"].String()) {
		t.Errorf("Sue, an administrator of Acme and Globex the operator did not name, granting Kim the role everywhere: "+
			"got %d (%s), want a 403 naming %s and her account id", status, body, platformSetting)
	}
	// Whatever the answer, a grant made from Acme does not make Kim an
	// administrator of Globex.
	if status, body := w.listEnvironments("kim", w.globex, w.globexProject); status != http.StatusForbidden {
		t.Errorf("after Sue's grant in Acme, Kim reading Globex's environments: got %d (%s), want 403", status, body)
	}

	// A role in their own organization is theirs to give.
	if status, body := w.grantHere("sue", "kim", w.acme, entities.RoleAdmin); status != http.StatusOK {
		t.Fatalf("Sue granting Kim the role in Acme: got %d (%s), want 200", status, body)
	}
}

// Somebody the operator names is a platform administrator, and grants a role
// that acts in every organization the account belongs to.
func TestAPlatformAdministratorGrantsARoleInEveryOrganization(t *testing.T) {
	w := newOrgRolesWorld(t, "root")
	w.account("root", []string{entities.RoleAdmin}, w.acme, w.globex)
	w.account("kim", nil, w.acme, w.globex)

	if status, body := w.grantEverywhere("root", "kim", w.acme, entities.RoleDesigner); status != http.StatusOK {
		t.Fatalf("a platform administrator granting a global role: got %d (%s), want 200", status, body)
	}
	for name, place := range map[string]struct{ organization, project uuid.UUID }{
		"Acme": {w.acme, w.acmeProject}, "Globex": {w.globex, w.globexProject},
	} {
		if status, body := w.deploy("kim", place.organization, place.project, "kim-"+strings.ToLower(name)); status != http.StatusOK {
			t.Errorf("Kim, a designer in every organization, deploying into %s: got %d (%s)", name, status, body)
		}
	}
}

// An administrator of Acme alone is not an administrator of every
// organization, whoever the operator names.
func TestARoleHeldInOneOrganizationDoesNotMakeAPlatformAdministrator(t *testing.T) {
	w := newOrgRolesWorld(t, "ana")
	w.account("ana", nil, w.acme)
	w.holdsIn("ana", w.acme, entities.RoleAdmin)
	w.account("lee", nil, w.acme)

	status, body := w.grantEverywhere("ana", "lee", w.acme, entities.RoleDesigner)
	refusedNaming(t, status, body, platformSetting)
}

// Creating an account holding a role in every organization, or deleting one
// that holds one, gives or takes that role everywhere too.
func TestCreatingOrDeletingAnAccountWithAGlobalRoleIsThePlatforms(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("sue", []string{entities.RoleAdmin}, w.acme, w.globex)
	w.account("dee", []string{entities.RoleDesigner}, w.acme)

	status, body := w.call("sue", http.MethodPost, "/api/v1/users", w.acme, map[string]any{
		"user": map[string]any{
			"username": "newcomer", "full_name": "Newcomer", "roles": []string{entities.RoleAdmin},
			"organizations": []map[string]string{{"id": w.acme.String()}},
		},
		"password": orgRolesPassword,
	})
	refusedNaming(t, status, body, platformSetting)

	status, body = w.call("sue", http.MethodDelete, "/api/v1/users/"+w.ids["dee"].String(), w.acme, nil)
	refusedNaming(t, status, body, platformSetting)

	// Without a global role, both are an organization's own business.
	if status, body := w.call("sue", http.MethodPost, "/api/v1/users", w.acme, map[string]any{
		"user": map[string]any{
			"username": "newcomer", "full_name": "Newcomer",
			"organizations": []map[string]string{{"id": w.acme.String()}},
		},
		"password": orgRolesPassword,
	}); status != http.StatusOK {
		t.Fatalf("creating an account with no role in every organization: got %d (%s), want 200", status, body)
	}
}

// A new account can hold roles in the organization it is created in from the
// start: the New account dialog names them, and nobody has to find the account
// again in the matrix to finish the job.
func TestAnAccountIsCreatedHoldingRolesInThisOrganization(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("ana", nil, w.acme)
	w.holdsIn("ana", w.acme, entities.RoleAdmin)

	status, body := w.call("ana", http.MethodPost, "/api/v1/users", w.acme, map[string]any{
		"user": map[string]any{
			"username": "newcomer", "full_name": "Newcomer", "organization_roles": []string{"designer"},
			"organizations": []map[string]string{{"id": w.acme.String()}},
		},
		"password": orgRolesPassword,
	})
	if status != http.StatusOK {
		t.Fatalf("Ana creating a designer in Acme: got %d (%s), want 200", status, body)
	}
	got := w.listed("ana", w.acme)["newcomer"]
	if !slices.Equal(got.OrganizationRoles, []string{entities.RoleDesigner}) || len(got.Roles) != 0 {
		t.Errorf("the new account holds %v in Acme and %v everywhere, want [DESIGNER] in Acme alone",
			got.OrganizationRoles, got.Roles)
	}
}

// There is no default account, so an organization whose last administrator
// stops being one cannot be administered through Metis again. Another
// organization's administrators are not this one's.
func TestTheLastAdministratorOfAnOrganizationCannotBeDemotedWhileAnotherHasAdministrators(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("ana", nil, w.acme)
	w.holdsIn("ana", w.acme, entities.RoleAdmin)
	w.account("bea", []string{entities.RoleAdmin}, w.globex)
	w.account("gil", nil, w.globex)
	w.holdsIn("gil", w.globex, entities.RoleAdmin)

	status, body := w.grantHere("ana", "ana", w.acme)
	refusedNaming(t, status, body, "ana is the last administrator of Acme")
	if status, body := w.listEnvironments("ana", w.acme, w.acmeProject); status != http.StatusOK {
		t.Fatalf("the refused demotion was made anyway: %d (%s)", status, body)
	}

	// With somebody else administering Acme, she may stop.
	w.account("kim", nil, w.acme)
	if status, body := w.grantHere("ana", "kim", w.acme, entities.RoleAdmin); status != http.StatusOK {
		t.Fatalf("make Kim an administrator of Acme: got %d (%s)", status, body)
	}
	if status, body := w.grantHere("ana", "ana", w.acme); status != http.StatusOK {
		t.Fatalf("Ana stepping down with Kim in place: got %d (%s), want 200", status, body)
	}
}

// Whoever administers an organization counts as its administrator, whether the
// role is held there or everywhere — so removing a global administrator is not
// refused while an administrator of the organization alone remains.
func TestAnAdministratorOfTheOrganizationAloneCountsAsAnother(t *testing.T) {
	w := newOrgRolesWorld(t, "root")
	w.account("root", []string{entities.RoleAdmin}, w.acme)
	w.account("kim", nil, w.acme)
	w.holdsIn("kim", w.acme, entities.RoleAdmin)

	if status, body := w.grantEverywhere("root", "root", w.acme); status != http.StatusOK {
		t.Fatalf("the global administrator stepping down while Kim administers Acme: got %d (%s), want 200", status, body)
	}
}

// A revoked role stops acting at the request after the revocation: the account
// a token names is read again once it changes, and the token itself carries no
// role that counts.
func TestRevokingARoleInAnOrganizationTakesEffectAtTheNextRequest(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("ana", nil, w.acme)
	w.holdsIn("ana", w.acme, entities.RoleAdmin)
	w.account("dee", nil, w.acme)

	if status, body := w.grantHere("ana", "dee", w.acme, entities.RoleDesigner); status != http.StatusOK {
		t.Fatalf("grant Dee Designer in Acme: got %d (%s)", status, body)
	}
	if status, body := w.deploy("dee", w.acme, w.acmeProject, "before-revocation"); status != http.StatusOK {
		t.Fatalf("Dee deploying as a designer: got %d (%s), want 200", status, body)
	}
	if status, body := w.grantHere("ana", "dee", w.acme); status != http.StatusOK {
		t.Fatalf("revoke Dee's role: got %d (%s)", status, body)
	}
	status, body := w.deploy("dee", w.acme, w.acmeProject, "after-revocation")
	refusedNaming(t, status, body, entities.RoleDesigner)
}

// What an organization's administrator may grant is the roles this
// installation has, to the accounts in their organization.
func TestAGrantNamesARoleThereIsToAnAccountThatIsHere(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("ana", nil, w.acme)
	w.holdsIn("ana", w.acme, entities.RoleAdmin)
	w.account("kim", nil, w.acme)
	w.account("bea", nil, w.globex)

	if status, body := w.grantHere("ana", "kim", w.acme, "SUPERUSER"); status != http.StatusBadRequest {
		t.Errorf("granting a role that does not exist: got %d (%s), want 400", status, body)
	}
	if status, body := w.grantHere("ana", "bea", w.acme, entities.RoleDesigner); status != http.StatusNotFound {
		t.Errorf("granting a role to an account in another organization: got %d (%s), want 404", status, body)
	}
	// Whatever case it is written in, a role is stored as the installation
	// spells it, once.
	if status, body := w.grantHere("ana", "kim", w.acme, "designer", entities.RoleDesigner); status != http.StatusOK {
		t.Fatalf("grant designer: got %d (%s)", status, body)
	}
	if got := w.listed("ana", w.acme)["kim"].OrganizationRoles; !slices.Equal(got, []string{entities.RoleDesigner}) {
		t.Errorf("Kim holds %v in Acme, want [DESIGNER]", got)
	}
}

// The interface asks whether the person looking may change roles held in every
// organization, to show them as fixed to everybody else.
func TestTheSignedInAccountIsToldWhetherItChangesGlobalRoles(t *testing.T) {
	w := newOrgRolesWorld(t, "root")
	w.account("root", []string{entities.RoleAdmin}, w.acme)
	w.account("sue", []string{entities.RoleAdmin}, w.acme, w.globex)
	w.account("ana", nil, w.acme)
	w.holdsIn("ana", w.acme, entities.RoleAdmin)

	for username, want := range map[string]bool{"root": true, "sue": false, "ana": false} {
		status, body := w.call(username, http.MethodGet, "/api/v1/users/me", w.acme, nil)
		if status != http.StatusOK {
			t.Fatalf("%s reading their own account: got %d (%s)", username, status, body)
		}
		var reply struct {
			User struct {
				OrganizationRoles []string `json:"organization_roles"`
			} `json:"user"`
			MayChangeGlobalRoles bool `json:"may_change_global_roles"`
		}
		if err := json.Unmarshal([]byte(body), &reply); err != nil {
			t.Fatalf("decode: %v (%s)", err, body)
		}
		if reply.MayChangeGlobalRoles != want {
			t.Errorf("%s may change global roles: %v, want %v", username, reply.MayChangeGlobalRoles, want)
		}
		if username == "ana" && !slices.Equal(reply.User.OrganizationRoles, []string{entities.RoleAdmin}) {
			t.Errorf("Ana is told she holds %v here, want [ADMIN]", reply.User.OrganizationRoles)
		}
	}
}
