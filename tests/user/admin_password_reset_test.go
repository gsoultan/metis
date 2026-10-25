package user_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// An administrator sets a colleague's password from the account form. Before
// this, a forgotten password on a running installation had one answer:
// --reset-password, run on the server.

const resetTo = "a-new-password-chosen-by-an-administrator"

// resetPassword sets target's password as somebody, for the organization given.
func (w *orgRolesWorld) resetPassword(as, target string, organization uuid.UUID, password string) (int, string) {
	w.t.Helper()
	return w.call(as, http.MethodPut, "/api/v1/users/"+w.ids[target].String()+"/password", organization,
		map[string]any{"new_password": password})
}

// signsInWith reports whether username's sign-in takes password.
func (w *orgRolesWorld) signsInWith(username, password string) bool {
	w.t.Helper()
	_, _, err := w.svc.Login(w.t.Context(), username, password)
	return err == nil
}

func TestAnAdministratorSetsAColleaguesPassword(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("ana", nil, w.acme)
	w.holdsIn("ana", w.acme, entities.RoleAdmin)
	w.account("kim", nil, w.acme)

	if status, body := w.resetPassword("ana", "kim", w.acme, resetTo); status != http.StatusOK {
		t.Fatalf("Ana, an administrator of Acme, setting Kim's password: got %d (%s), want 200", status, body)
	}
	if !w.signsInWith("kim", resetTo) {
		t.Fatal("Kim cannot sign in with the password Ana set")
	}
	if w.signsInWith("kim", orgRolesPassword) {
		t.Fatal("Kim's old password still works, so the new one was added rather than replacing it")
	}
}

// Every refusal leaves the password as it was.
func TestWhoMayNotSetAnAccountsPassword(t *testing.T) {
	w := newOrgRolesWorld(t, "plat")
	w.account("ana", nil, w.acme)
	w.holdsIn("ana", w.acme, entities.RoleAdmin)
	w.account("kim", nil, w.acme)
	w.account("lee", nil, w.acme)
	w.account("gus", nil, w.globex)
	w.account("sam", nil, w.acme, w.globex)
	w.account("plat", []string{entities.RoleAdmin}, w.acme, w.globex)
	w.account("root", []string{entities.RoleAdmin}, w.acme)

	cases := []struct {
		name, as, target string
		password         string
		want             int
	}{
		// The endpoint gate: a member who administers nothing.
		{"a member who is not an administrator", "lee", "kim", resetTo, http.StatusForbidden},
		// ChangePassword asks for the current password; this must not be the
		// way round it for somebody holding a stolen session.
		{"an administrator's own account", "ana", "ana", resetTo, http.StatusForbidden},
		// Another organization's account reads as not there at all.
		{"an account in another organization", "ana", "gus", resetTo, http.StatusNotFound},
		// Its sign-in is Globex's too, and Ana does not administer Globex.
		{"an account another organization shares", "ana", "sam", resetTo, http.StatusForbidden},
		// Root's sign-in reaches every organization root belongs to.
		{"an account holding a role in every organization", "ana", "root", resetTo, http.StatusForbidden},
		{"a password shorter than the minimum", "ana", "kim", "short", http.StatusBadRequest},
		{"a password longer than bcrypt reads", "ana", "kim", strings.Repeat("x", 73), http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if status, body := w.resetPassword(c.as, c.target, w.acme, c.password); status != c.want {
				t.Fatalf("got %d (%s), want %d", status, body, c.want)
			}
			if !w.signsInWith(c.target, orgRolesPassword) {
				t.Fatalf("%s's password changed although the reset was refused", c.target)
			}
		})
	}

	// A platform administrator may take what an organization's may not.
	if status, body := w.resetPassword("plat", "root", w.acme, resetTo); status != http.StatusOK {
		t.Fatalf("a platform administrator setting root's password: got %d (%s), want 200", status, body)
	}
	if !w.signsInWith("root", resetTo) {
		t.Fatal("root cannot sign in with the password the platform administrator set")
	}
}

// Its password is the provider's: one set here would be a second way in that
// the provider does not control.
func TestAnAccountAProviderSignsInHasNoPasswordToSet(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("ana", nil, w.acme)
	w.holdsIn("ana", w.acme, entities.RoleAdmin)
	w.account("kim", nil, w.acme)
	if err := w.db.WithContext(t.Context()).Exec(
		`UPDATE users SET identity_issuer = ?, identity_subject = ? WHERE id = ?`,
		"https://id.example.com", "kim-subject", w.ids["kim"]).Error; err != nil {
		t.Fatalf("link Kim to a provider: %v", err)
	}

	status, body := w.resetPassword("ana", "kim", w.acme, resetTo)
	if status != http.StatusBadRequest || !strings.Contains(body, "https://id.example.com") {
		t.Fatalf("got %d (%s), want 400 naming the provider", status, body)
	}
}

// Taking somebody's sign-in is said in the log as a change of role is: who did
// it, whose account it was. The password is not.
func TestAPasswordAnAdministratorSetsIsLoggedWithWhoSetIt(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("ana", nil, w.acme)
	w.holdsIn("ana", w.acme, entities.RoleAdmin)
	w.account("kim", nil, w.acme)
	logs := captureLogs(t)

	if status, body := w.resetPassword("ana", "kim", w.acme, resetTo); status != http.StatusOK {
		t.Fatalf("set Kim's password: got %d (%s)", status, body)
	}
	lines := logs.said("An account's password was set by an administrator")
	if len(lines) != 1 {
		t.Fatalf("the reset logged %d lines, want one: %v", len(lines), lines)
	}
	line := lines[0]
	if line["actor"] != "ana" || line["actor_id"] != w.ids["ana"].String() ||
		line["target"] != "kim" || line["target_id"] != w.ids["kim"].String() || line["organization"] != w.acme.String() {
		t.Fatalf("the line is %v; want it to name Ana, Kim and Acme", line)
	}
	for _, kept := range logs.said("") {
		if raw, _ := json.Marshal(kept); strings.Contains(string(raw), resetTo) {
			t.Fatalf("the log holds the password: %v", kept)
		}
	}

	// A refused one says nothing.
	logs = captureLogs(t)
	if status, _ := w.resetPassword("ana", "ana", w.acme, resetTo); status == http.StatusOK {
		t.Fatal("Ana set her own password without the current one")
	}
	if lines := logs.said("An account's password was set by an administrator"); len(lines) != 0 {
		t.Fatalf("a refused reset was logged: %v", lines)
	}
}
