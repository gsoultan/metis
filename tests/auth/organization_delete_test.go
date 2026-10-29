package auth_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// An organization is deleted once nobody else is in it and nothing runs in it.
//
// Deleting one only hid it: its members went on signing in to it and listing
// its projects, and its work went on running — on one administrator's click.
// Now the delete is refused with a 400 that names what is still there, and
// once that is gone the organization goes, and its administrator with it.
func TestAnOrganizationIsDeletedOnlyOnceItIsEmpty(t *testing.T) {
	h := newLocalHarness(t)
	acme := h.organization("Acme")
	grace := h.localAccount("grace", "grace@acme.example", acme)
	h.holdsIn(grace.ID, acme, entities.RoleAdmin)
	hopper := h.localAccount("hopper", "hopper@acme.example", acme)
	token := h.login("grace")
	path := "/api/v1/organizations/" + acme.String()

	status, body := h.do(http.MethodDelete, path, token, acme.String(), nil)
	if status != http.StatusBadRequest || !strings.Contains(body, "1 other member and 1 project") {
		t.Fatalf("delete Acme with hopper and a project in it: status %d (%s), want 400 naming both", status, body)
	}
	if status, body := h.get(path, token, acme.String()); status != http.StatusOK {
		t.Fatalf("Acme after a refused delete: status %d (%s), want it still there", status, body)
	}

	ctx := inOrganization(t.Context(), acme)
	if err := h.svc.DeleteUser(ctx, hopper.ID); err != nil {
		t.Fatalf("remove hopper: %v", err)
	}
	status, body = h.do(http.MethodDelete, path, token, acme.String(), nil)
	if status != http.StatusBadRequest || !strings.Contains(body, "1 project") || strings.Contains(body, "member") {
		t.Fatalf("delete Acme with a project in it: status %d (%s), want 400 naming the project alone", status, body)
	}

	projects, err := h.svc.ListProjects(ctx, acme)
	if err != nil || len(projects) != 1 {
		t.Fatalf("list Acme's projects: %v (%d)", err, len(projects))
	}
	if err := h.svc.DeleteProject(ctx, projects[0].ID); err != nil {
		t.Fatalf("delete Acme's project: %v", err)
	}
	if status, body := h.do(http.MethodDelete, path, token, acme.String(), nil); status != http.StatusOK {
		t.Fatalf("delete an empty Acme: status %d (%s), want 200", status, body)
	}
	if members := h.members(acme); len(members) != 0 {
		t.Fatalf("Acme's members after it was deleted: %v, want none", members)
	}
}
