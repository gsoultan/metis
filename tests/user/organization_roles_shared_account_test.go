package user_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// An account is the same in every organization it belongs to: renaming it,
// changing its email or deleting it acts in all of them. The check in front of
// those changes asked whether the caller was a member of every organization
// the account is in. While roles were global that meant "administers every
// one of them"; with roles held in one organization it does not. Ana,
// administrator of Acme and an ordinary member of Globex, could rename or
// delete Gus, Globex's administrator.
func TestASharedAccountIsChangedOnlyByWhoAdministersEveryOrganizationItIsIn(t *testing.T) {
	w := newOrgRolesWorld(t)
	w.account("ana", nil, w.acme, w.globex)
	w.holdsIn("ana", w.acme, entities.RoleAdmin)
	gus := w.account("gus", nil, w.acme, w.globex)
	w.holdsIn("gus", w.globex, entities.RoleAdmin)
	// Globex has another administrator, so no last-administrator rule is what
	// stands in the way below.
	w.account("bea", nil, w.globex)
	w.holdsIn("bea", w.globex, entities.RoleAdmin)

	rename := map[string]any{"username": "gus", "full_name": "Renamed by Ana", "email": "ana-now-reads-this@example.com"}
	status, body := w.call("ana", http.MethodPut, "/api/v1/users/"+gus.String(), w.acme, rename)
	refusedAsNotTheirs(t, "Ana renaming Globex's administrator", status, body)
	if name := w.fullName(gus); name != "gus" {
		t.Fatalf("the refused change was made anyway: Gus is now called %q", name)
	}

	status, body = w.call("ana", http.MethodDelete, "/api/v1/users/"+gus.String(), w.acme, nil)
	refusedAsNotTheirs(t, "Ana deleting Globex's administrator", status, body)

	// Somebody who administers both organizations may.
	w.account("dee", nil, w.acme, w.globex)
	w.holdsIn("dee", w.acme, entities.RoleAdmin)
	w.holdsIn("dee", w.globex, entities.RoleAdmin)
	rename["full_name"] = "Renamed by Dee"
	if status, body := w.call("dee", http.MethodPut, "/api/v1/users/"+gus.String(), w.acme, rename); status != http.StatusOK {
		t.Fatalf("Dee, an administrator of Acme and Globex, renaming Gus: got %d (%s), want 200", status, body)
	}
}

// refusedAsNotTheirs is a 403 that says an administrator of the other
// organization has to make the change, without naming it.
func refusedAsNotTheirs(t *testing.T, what string, status int, body string) {
	t.Helper()
	if status != http.StatusForbidden {
		t.Fatalf("%s: got %d (%s), want 403", what, status, body)
	}
	if !strings.Contains(body, "an administrator there has to make this change") {
		t.Fatalf("%s was refused without saying who can make the change: %s", what, body)
	}
}

// fullName is what the database holds, whatever any response said.
func (w *orgRolesWorld) fullName(id uuid.UUID) string {
	w.t.Helper()
	var name string
	if err := w.db.WithContext(w.t.Context()).Raw(`SELECT full_name FROM users WHERE id = ?`, id).Row().Scan(&name); err != nil {
		w.t.Fatalf("read %s's name: %v", id, err)
	}
	return name
}
