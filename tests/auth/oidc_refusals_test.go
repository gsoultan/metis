package auth_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Somebody the identity provider vouched for, whom nothing places in an
// organization, is refused with 403 — they are authenticated, just not
// admitted — and told which setting or claim is missing. A 401 would send
// them round the sign-in again, to be refused again, with nothing to tell
// anybody what to fix. No account is created for them.

func TestWithTheSettingUnsetAnOIDCUserIsRefusedWith403NamingIt(t *testing.T) {
	h := newOIDCHarness(t, "")
	acme := h.organization("Acme")
	token := h.provider.token(t, "subject-ada", map[string]any{
		"preferred_username": "ada",
		organizationClaim:    []string{acme.String()},
	})

	refusal := assertRefused(t, h, "the setting unset", token, envOrganizationClaim)
	if !strings.Contains(refusal, "operator") {
		t.Errorf("the refusal does not say whose fix it is: %s", refusal)
	}
	if got := h.members(acme); len(got) != 0 {
		t.Fatalf("an account was created for somebody who was refused: %v", got)
	}
}

func TestAnOIDCUserWhoseTokenLacksTheClaimIsRefusedWith403NamingIt(t *testing.T) {
	h := newOIDCHarness(t, organizationClaim)
	acme := h.organization("Acme")
	token := h.provider.token(t, "subject-ada", map[string]any{"preferred_username": "ada"})

	assertRefused(t, h, "no claim in the token", token, organizationClaim)
	if got := h.members(acme); len(got) != 0 {
		t.Fatalf("an account was created for somebody who was refused: %v", got)
	}
}

func TestAnOIDCUserWhoseClaimNamesNoOrganizationHereIsRefusedWith403NamingIt(t *testing.T) {
	h := newOIDCHarness(t, organizationClaim)
	acme := h.organization("Acme")
	gone := h.organization("Initech")
	if err := h.svc.DeleteOrganization(inOrganization(t.Context(), gone), gone); err != nil {
		t.Fatalf("delete Initech: %v", err)
	}

	// Not subtests: the harness reports through the test it was built for.
	for name, named := range map[string][]string{
		"an id nothing has":             {uuid.NewString()},
		"a name rather than an id":      {"Acme"},
		"an organization since deleted": {gone.String()},
		"an empty list":                 {},
		"several, none of them here":    {"", "not-an-id", uuid.NewString()},
	} {
		token := h.provider.token(t, "subject-"+uuid.NewString(), map[string]any{
			"preferred_username": "ada",
			organizationClaim:    named,
		})
		assertRefused(t, h, name, token, organizationClaim)
	}
	if got := h.members(acme); len(got) != 0 {
		t.Fatalf("an account was created for somebody who was refused: %v", got)
	}
}

// assertRefused checks a request is answered 403 with an error that names
// what is missing, and returns the error.
func assertRefused(t *testing.T, h *apiHarness, when, token, names string) string {
	t.Helper()
	status, body := h.get("/api/v1/projects", token, "")
	if status != http.StatusForbidden {
		t.Fatalf("%s: status %d (%s), want 403: the person is authenticated, just not admitted", when, status, body)
	}
	var reply struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &reply); err != nil || reply.Error == "" {
		t.Fatalf("%s: the refusal carries no error a client can show: %q", when, body)
	}
	if !strings.Contains(reply.Error, names) {
		t.Fatalf("%s: the refusal does not name %s: %s", when, names, reply.Error)
	}
	return reply.Error
}
