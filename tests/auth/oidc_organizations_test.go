package auth_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"
)

// An OIDC user whose claim names an organization reaches that organization, as
// the account their first sign-in created — and no other organization.
//
// Before, every OIDC user was refused on every organization-scoped endpoint:
// the token's claims carry no membership list, so the tenant resolver could
// not place them anywhere and answered 401.
func TestAnOIDCUserReachesTheOrganizationTheirClaimNames(t *testing.T) {
	h := newOIDCHarness(t, organizationClaim)
	acme := h.organization("Acme")
	globex := h.organization("Globex")

	token := h.provider.token(t, "subject-ada", map[string]any{
		"preferred_username": "ada",
		"email":              "ada@acme.example",
		organizationClaim:    []string{acme.String()},
	})

	if got := h.projects(token, ""); !slices.Equal(got, []string{"Acme Project"}) {
		t.Fatalf("Ada reached projects %v, want Acme's alone", got)
	}
	// Asking for another organization is a selection among memberships, and
	// the claim gave her none there.
	if status, body := h.get("/api/v1/projects", token, globex.String()); status == http.StatusOK {
		t.Fatalf("Ada reached Globex, which her claim does not name: %s", body)
	}

	account := h.me(token)
	if account.Username != "ada" {
		t.Errorf("the account is called %q, want the provider's preferred username", account.Username)
	}
	if account.IdentityProvider != h.provider.issuer() {
		t.Errorf("the account says it signs in through %q, want %q", account.IdentityProvider, h.provider.issuer())
	}
	// No role: the task inbox needs none, and anything more is an
	// administrator's decision, not a side effect of signing in.
	if len(account.Roles) != 0 {
		t.Errorf("a first sign-in was given roles %v, want none", account.Roles)
	}
	if got := h.members(acme); !slices.Equal(got, []string{"ada"}) {
		t.Errorf("Acme's members are %v, want Ada", got)
	}
	if got := h.members(globex); len(got) != 0 {
		t.Errorf("Globex's members are %v, want nobody", got)
	}
}

// A second sign-in is the same account, found again by the issuer and subject
// the provider vouched for. An email address links nothing: not to a local
// account that has it, and not between two subjects that both assert it.
func TestASecondSignInIsTheSameAccountAndAnEmailLinksNothing(t *testing.T) {
	h := newOIDCHarness(t, organizationClaim)
	acme := h.organization("Acme")
	local := h.localAccount("grace", "grace@acme.example", acme)

	claims := map[string]any{
		"preferred_username": "grace",
		"email":              "grace@acme.example",
		// One organization as a bare string, which a claim may also be.
		organizationClaim: acme.String(),
	}
	first := h.me(h.provider.token(t, "subject-grace", claims))
	second := h.me(h.provider.token(t, "subject-grace", claims))

	if first.ID != second.ID {
		t.Fatalf("two sign-ins by one identity reached two accounts: %s and %s", first.ID, second.ID)
	}
	if first.ID == local.ID.String() || first.Username == local.Username {
		t.Fatalf("the sign-in was linked to the local account %q by its email or username", local.Username)
	}
	if first.IdentityProvider != h.provider.issuer() {
		t.Errorf("the account says it signs in through %q, want %q", first.IdentityProvider, h.provider.issuer())
	}

	impostor := h.me(h.provider.token(t, "subject-someone-else", claims))
	if impostor.ID == first.ID || impostor.ID == local.ID.String() {
		t.Fatal("a different subject asserting the same email reached somebody else's account")
	}

	if got := h.members(acme); len(got) != 3 {
		t.Fatalf("Acme holds %v, want the local account and one account per identity", got)
	}
}

// An organization the claim no longer names is no longer reachable, even though
// an earlier sign-in's claim made the account its member — and the account is
// no longer listed among its members.
func TestAnOrganizationTheClaimNoLongerNamesIsNoLongerAdmitted(t *testing.T) {
	h := newOIDCHarness(t, organizationClaim)
	acme := h.organization("Acme")
	globex := h.organization("Globex")

	both := h.provider.token(t, "subject-linus", map[string]any{
		"preferred_username": "linus",
		organizationClaim:    []string{acme.String(), globex.String()},
	})
	if got := h.projects(both, globex.String()); !slices.Equal(got, []string{"Globex Project"}) {
		t.Fatalf("with Globex in the claim, Linus reached %v", got)
	}
	if got := h.members(globex); !slices.Equal(got, []string{"linus"}) {
		t.Fatalf("Globex's members are %v, want Linus", got)
	}

	// The provider takes Globex out of his claim.
	acmeOnly := h.provider.token(t, "subject-linus", map[string]any{
		"preferred_username": "linus",
		organizationClaim:    []string{acme.String()},
	})
	if status, body := h.get("/api/v1/projects", acmeOnly, globex.String()); status == http.StatusOK {
		t.Fatalf("Linus still reached Globex after his claim stopped naming it: %s", body)
	}
	if got := h.projects(acmeOnly, ""); !slices.Equal(got, []string{"Acme Project"}) {
		t.Fatalf("Linus reached %v, want Acme's projects", got)
	}
	if got := h.members(globex); len(got) != 0 {
		t.Fatalf("Globex still lists %v as members after the claim stopped naming it", got)
	}
	if got := h.members(acme); !slices.Equal(got, []string{"linus"}) {
		t.Fatalf("Acme's members are %v, want Linus", got)
	}
}

// An account that signs in through the provider has no password here, and is
// told so — not told that the password it typed was wrong.
func TestAnOIDCAccountIsSentToItsProviderToChangeItsPassword(t *testing.T) {
	h := newOIDCHarness(t, organizationClaim)
	acme := h.organization("Acme")
	token := h.provider.token(t, "subject-edsger", map[string]any{
		"preferred_username": "edsger",
		organizationClaim:    []string{acme.String()},
	})

	status, body := h.do(http.MethodPost, "/api/v1/users/me/password", token, "", map[string]string{
		"current_password": "whatever-they-think-it-is",
		"new_password":     "a-brand-new-password",
	})
	if status != http.StatusBadRequest || !strings.Contains(body, "identity provider") {
		t.Fatalf("changing the password: status %d (%s), want 400 sending them to their identity provider", status, body)
	}
}

// A local account signs in and is scoped exactly as before, with or without
// OIDC configured.
func TestALocalAccountsAccessIsUnchanged(t *testing.T) {
	t.Run("without OIDC", func(t *testing.T) {
		h := newLocalHarness(t)
		acme := h.organization("Acme")
		globex := h.organization("Globex")
		h.localAccount("hopper", "hopper@acme.example", acme)
		token := h.login("hopper")

		if got := h.projects(token, ""); !slices.Equal(got, []string{"Acme Project"}) {
			t.Fatalf("Hopper reached %v, want Acme's projects", got)
		}
		if status, body := h.get("/api/v1/projects", token, globex.String()); status != http.StatusUnauthorized {
			t.Fatalf("Hopper asking for Globex: status %d (%s), want the 401 a non-member has always had", status, body)
		}
		account := h.me(token)
		if account.IdentityProvider != "" || !slices.Equal(account.Roles, []string{"DESIGNER"}) {
			t.Fatalf("Hopper's account changed: %+v", account)
		}
	})

	// With OIDC configured the API accepts the provider's tokens and nothing
	// else, as it always has: a local account's token is refused.
	t.Run("with OIDC configured", func(t *testing.T) {
		h := newOIDCHarness(t, organizationClaim)
		acme := h.organization("Acme")
		h.localAccount("hopper", "hopper@acme.example", acme)
		token := h.login("hopper")

		if status, body := h.get("/api/v1/projects", token, ""); status != http.StatusUnauthorized {
			t.Fatalf("a local token under OIDC: status %d (%s), want 401 as before", status, body)
		}
	})
}
