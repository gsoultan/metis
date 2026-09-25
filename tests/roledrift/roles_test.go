// Package roledrift asserts that the roles the browser offers are roles the
// server recognises.
//
// It exists because they were not. The picker on the Platform access page
// offered admin, manager, developer, user and viewer; the server's vocabulary
// is ADMIN, DESIGNER and OPERATOR. Only admin matched, and only because HasRole
// compares case-insensitively. Granting somebody "developer" looked like
// letting them deploy process models and did nothing at all — a permission an
// administrator believed they had given, silently absent.
//
// The check reads the UI's own list rather than a copy of it, because a copy is
// the thing that drifted.
package roledrift

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
)

// rolesFile is the single list the UI offers, relative to the repository root.
const rolesFile = "../../ui/src/domain/roles.ts"

// offered matches the `value: 'x',` of each entry in ROLE_OPTIONS.
var offered = regexp.MustCompile(`value:\s*'([^']+)'`)

func TestEveryRoleTheUIOffersIsOneTheServerEnforces(t *testing.T) {
	source, err := os.ReadFile(filepath.Clean(rolesFile))
	if err != nil {
		t.Fatalf("could not read %s: %v", rolesFile, err)
	}

	matches := offered.FindAllStringSubmatch(string(source), -1)
	if len(matches) == 0 {
		// No matches means the file changed shape, not that it is clean. A
		// regression test that silently stops testing is worse than none.
		t.Fatalf("found no role values in %s; the file's shape changed and this check no longer reads it", rolesFile)
	}

	server := []string{entities.RoleAdmin, entities.RoleDesigner, entities.RoleOperator, entities.RoleQueryAuthor}
	for _, match := range matches {
		role := match[1]
		if !entities.HasRole(server, role) {
			t.Errorf("the UI offers %q, which no server-side check accepts: granting it grants nothing", role)
		}
	}
}

// TestEveryRoleTheServerEnforcesIsOneTheUIOffers is the other direction: a role
// nobody can grant from the only screen that grants roles is a role nobody has.
func TestEveryRoleTheServerEnforcesIsOneTheUIOffers(t *testing.T) {
	source, err := os.ReadFile(filepath.Clean(rolesFile))
	if err != nil {
		t.Fatalf("could not read %s: %v", rolesFile, err)
	}

	var ui []string
	for _, match := range offered.FindAllStringSubmatch(string(source), -1) {
		ui = append(ui, match[1])
	}

	for _, role := range []string{entities.RoleAdmin, entities.RoleDesigner, entities.RoleOperator, entities.RoleQueryAuthor} {
		if !entities.HasRole(ui, role) {
			t.Errorf("the server enforces %q, which the UI never offers: nobody can be granted it", role)
		}
	}
}

// TestBuiltInRolesAreTheEnforcedVocabulary keeps the seeded rows and the
// constants the interceptors compare against from becoming two lists.
func TestBuiltInRolesAreTheEnforcedVocabulary(t *testing.T) {
	seeded := entities.BuiltInPlatformRoles()
	if len(seeded) != 4 {
		t.Fatalf("expected four built-in roles, got %d", len(seeded))
	}
	var names []string
	for _, role := range seeded {
		names = append(names, role.Name)
		if role.Description == "" {
			t.Errorf("the %s role has no description; the picker would show a bare token", role.Name)
		}
	}
	for _, want := range []string{entities.RoleAdmin, entities.RoleDesigner, entities.RoleOperator, entities.RoleQueryAuthor} {
		if !entities.HasRole(names, want) {
			t.Errorf("%q is enforced but not seeded: no account could ever hold it", want)
		}
	}
}
