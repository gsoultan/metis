package user_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// Who administers an organization is changed through the account service:
// an account is created holding a role, a role is given or taken away, an
// account is deleted. Nothing else records who did it, so the server's log
// does — one line for each such change, naming who made it and whose account
// it was, by id as well as by name, the organization the request was for,
// and the roles the account held before and after: on the account, and in
// each organization alone. Nothing else is in the line: no password, no
// token, no name or address.
//
// It is what an auditor has for a second administrator made, used and
// removed around an approval — or removed and restored around one.
func TestEveryChangeToWhoHoldsWhichRoleIsLoggedWithWhoMadeIt(t *testing.T) {
	w := newOrgRolesWorld(t, "root")
	root := w.account("root", []string{entities.RoleAdmin}, w.acme, w.globex)
	logs := captureLogs(t)
	const secret = "a-password-nobody-may-read-in-a-log"

	// change is the one line logged about target since the last one read.
	read := 0
	change := func(fragment string, target uuid.UUID) map[string]any {
		t.Helper()
		lines := logs.said(fragment)
		var about []map[string]any
		for _, line := range lines {
			if line["target_id"] == target.String() {
				about = append(about, line)
			}
		}
		if len(about) != read+1 && fragment != "" {
			about = nil
			for _, line := range logs.said("") {
				if line["target_id"] == target.String() {
					about = append(about, line)
				}
			}
		}
		if len(about) != read+1 {
			t.Fatalf("%d lines name account %s as changed, want %d: %v", len(about), target, read+1, about)
		}
		read++
		line := about[len(about)-1]
		if !strings.Contains(line["message"].(string), fragment) {
			t.Fatalf("the line reads %q, want it to say %q", line["message"], fragment)
		}
		return line
	}
	expect := func(what string, line map[string]any, target uuid.UUID, name string, organization uuid.UUID, want map[string]any) {
		t.Helper()
		if line["level"] != "info" || line["actor_id"] != root.String() || line["actor"] != "root" ||
			line["target_id"] != target.String() || line["target"] != name || line["organization"] != organization.String() {
			t.Fatalf("%s: the line %v does not name root as who made it, %s (%s) as whose it was, and organization %s", what, line, name, target, organization)
		}
		for field, roles := range want {
			if !reflect.DeepEqual(line[field], roles) {
				t.Fatalf("%s: %s is %v, want %v (the line: %v)", what, field, line[field], roles, line)
			}
		}
		// The line holds what is listed and nothing else.
		allowed := []string{"level", "time", "message", "actor_id", "actor", "target_id", "target", "organization",
			"roles_before", "roles_after", "organization_roles_before", "organization_roles_after"}
		for field := range line {
			if !slices.Contains(allowed, field) {
				t.Fatalf("%s: the line carries %q, which is not who, whose, where or which roles: %v", what, field, line)
			}
		}
		written, _ := json.Marshal(line)
		for _, private := range []string{secret, "password", "token", "hash", "Second Person", "second@example.com"} {
			if strings.Contains(string(written), private) {
				t.Fatalf("%s: the line carries %q: %s", what, private, written)
			}
		}
	}
	none, nowhere := []any{}, map[string]any{}

	// An account is created holding a role on the account and one in Acme.
	status, body := w.call("root", http.MethodPost, "/api/v1/users", w.acme, map[string]any{
		"user": map[string]any{
			"username": "second", "full_name": "Second Person", "email": "second@example.com",
			"roles": []string{entities.RoleOperator}, "organization_roles": []string{entities.RoleAdmin},
			"organizations": []map[string]string{{"id": w.acme.String()}},
		},
		"password": secret,
	})
	if status != http.StatusOK {
		t.Fatalf("root creates an administrator of Acme: %d (%s)", status, body)
	}
	var second uuid.UUID
	if err := w.db.Raw(`SELECT id FROM users WHERE username = 'second'`).Row().Scan(&second); err != nil {
		t.Fatalf("read the new account's id: %v", err)
	}
	w.ids["second"] = second
	expect("creating an account", change("An account was created", second), second, "second", w.acme, map[string]any{
		"roles_before": none, "roles_after": []any{entities.RoleOperator},
		"organization_roles_before": nowhere, "organization_roles_after": map[string]any{w.acme.String(): []any{entities.RoleAdmin}},
	})

	// Its role in Acme is taken away, and given back.
	if status, body := w.grantHere("root", "second", w.acme); status != http.StatusOK {
		t.Fatalf("root takes the role in Acme away: %d (%s)", status, body)
	}
	expect("taking a role in an organization away", change("roles in an organization were changed", second), second, "second", w.acme, map[string]any{
		"roles_before": []any{entities.RoleOperator}, "roles_after": []any{entities.RoleOperator},
		"organization_roles_before": map[string]any{w.acme.String(): []any{entities.RoleAdmin}}, "organization_roles_after": nowhere,
	})
	if status, body := w.grantHere("root", "second", w.acme, entities.RoleAdmin, entities.RoleDesigner); status != http.StatusOK {
		t.Fatalf("root gives roles in Acme: %d (%s)", status, body)
	}
	expect("giving roles in an organization", change("roles in an organization were changed", second), second, "second", w.acme, map[string]any{
		"organization_roles_before": nowhere,
		"organization_roles_after":  map[string]any{w.acme.String(): []any{entities.RoleAdmin, entities.RoleDesigner}},
	})

	// The role on the account is changed.
	if status, body := w.grantEverywhere("root", "second", w.acme, entities.RoleAdmin); status != http.StatusOK {
		t.Fatalf("root changes the role on the account: %d (%s)", status, body)
	}
	held := map[string]any{w.acme.String(): []any{entities.RoleAdmin, entities.RoleDesigner}}
	expect("changing the roles on an account", change("An account's roles were changed", second), second, "second", w.acme, map[string]any{
		"roles_before": []any{entities.RoleOperator}, "roles_after": []any{entities.RoleAdmin},
		"organization_roles_before": held, "organization_roles_after": held,
	})
	// An update that changes no role — a name — is not a change to who holds
	// which role, and says nothing.
	if status, body := w.call("root", http.MethodPut, "/api/v1/users/"+second.String(), w.acme, map[string]any{
		"user": map[string]any{"full_name": "Second Person", "email": "second@example.com", "roles": []string{entities.RoleAdmin}},
	}); status != http.StatusOK {
		t.Fatalf("root renames the account: %d (%s)", status, body)
	}
	if lines := logs.said("An account's roles were changed"); len(lines) != 1 {
		t.Fatalf("an update that changed no role was logged as a change of roles: %v", lines)
	}

	// The account is deleted.
	if status, body := w.call("root", http.MethodDelete, "/api/v1/users/"+second.String(), w.acme, nil); status != http.StatusOK {
		t.Fatalf("root deletes the account: %d (%s)", status, body)
	}
	expect("deleting an account", change("An account was deleted", second), second, "second", w.acme, map[string]any{
		"roles_before": []any{entities.RoleAdmin}, "roles_after": none,
		"organization_roles_before": held, "organization_roles_after": nowhere,
	})
}

// A change that is refused changed nothing, and is not logged as one.
func TestARefusedChangeToAnAccountIsNotLoggedAsAChange(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("ana", nil, w.acme)
	w.holdsIn("ana", w.acme, entities.RoleAdmin)
	w.account("dee", nil, w.acme)
	logs := captureLogs(t)

	// Ana is the only administrator of Acme: taking her own role away is
	// refused, and so is deleting her account.
	if status, body := w.grantHere("ana", "ana", w.acme); status == http.StatusOK {
		t.Fatalf("the only administrator giving up the role: %d (%s), want it refused", status, body)
	}
	if status, body := w.call("ana", http.MethodDelete, "/api/v1/users/"+w.ids["ana"].String(), w.acme, nil); status == http.StatusOK {
		t.Fatalf("the only administrator deleting their account: %d (%s), want it refused", status, body)
	}
	// A role on the account is the platform's to give where there are two
	// organizations: refused.
	if status, body := w.grantEverywhere("ana", "dee", w.acme, entities.RoleAdmin); status == http.StatusOK {
		t.Fatalf("an organization's administrator giving a role in every organization: %d (%s), want it refused", status, body)
	}
	for _, fragment := range []string{"An account was created", "roles were changed", "roles in an organization were changed", "An account was deleted"} {
		if lines := logs.said(fragment); len(lines) != 0 {
			t.Fatalf("a refused change was logged: %v", lines)
		}
	}
}

// A change made with nobody signed in — the installation's own first
// account, seeded as system work — names no actor, and says so by leaving
// both fields empty: nothing is invented.
func TestAnAccountChangedByTheServerItselfNamesNoActor(t *testing.T) {
	w := newOrgRolesWorld(t)
	logs := captureLogs(t)
	id := w.account("first", []string{entities.RoleAdmin}, w.acme)
	var created []map[string]any
	for _, line := range logs.said("An account was created") {
		if line["target_id"] == id.String() {
			created = append(created, line)
		}
	}
	if len(created) != 1 || created[0]["actor_id"] != "" || created[0]["actor"] != "" || created[0]["target"] != "first" ||
		!reflect.DeepEqual(created[0]["roles_after"], []any{entities.RoleAdmin}) {
		t.Fatalf("the line for an account the server seeded: %v, want one, naming no actor", created)
	}
	if _, placed := created[0]["organization"]; placed {
		t.Fatalf("a change made for no organization names one: %v", created[0])
	}
}
