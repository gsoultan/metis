package roledrift

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/metis/internal/pkg/auth"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/endpoints/role"
)

// A role is described twice in words — the picker's sentence in roles.ts and
// the seeded row's description — and both said the Operator migrates running
// instances. The gate on MigrateInstances is adminOnly, so an administrator
// granting Operator for somebody to migrate running work would see the
// migration refused. A description may say a role migrates instances only when
// the role's gate admits it.
func TestNoRoleIsSaidToMigrateInstancesUnlessItsGateAdmitsIt(t *testing.T) {
	legend := servedLegend(t)
	descriptions := uiDescriptions(t)
	for _, seeded := range entities.BuiltInPlatformRoles() {
		admitted := slices.Contains(legend[seeded.Name], "MigrateInstances")
		for source, text := range map[string]string{
			"roles.ts":             descriptions[seeded.Name],
			"BuiltInPlatformRoles": seeded.Description,
		} {
			if strings.Contains(strings.ToLower(text), "migrat") && !admitted {
				t.Errorf("%s describes %s as migrating instances (%q), and its gate does not admit MigrateInstances",
					source, seeded.Name, text)
			}
		}
	}
}

// uiDescriptions reads each role's sentence from the picker's own list.
func uiDescriptions(t *testing.T) map[string]string {
	t.Helper()
	source, err := os.ReadFile(filepath.Clean(rolesFile))
	if err != nil {
		t.Fatalf("could not read %s: %v", rolesFile, err)
	}
	entry := regexp.MustCompile(`value:\s*'([^']+)'[^}]*?description:\s*'([^']*)'`)
	found := map[string]string{}
	for _, match := range entry.FindAllStringSubmatch(string(source), -1) {
		found[match[1]] = match[2]
	}
	if len(found) == 0 {
		t.Fatalf("found no role descriptions in %s; the file's shape changed and this check no longer reads it", rolesFile)
	}
	return found
}

// servedLegend is what GET /api/v1/roles answers, as role → methods, from the
// gates MakeEndpoints builds.
func servedLegend(t *testing.T) map[string][]string {
	t.Helper()
	ctx := context.WithValue(t.Context(), pkgauth.UserContextKey, entities.User{
		ID:            uuid.Must(uuid.NewV7()),
		Username:      "reader",
		Organizations: []*entities.Organization{{ID: uuid.Must(uuid.NewV7())}},
	})
	reply, err := endpoints.MakeEndpoints(nil).Role.ListRoles(ctx, nil)
	if err != nil {
		t.Fatalf("list roles: %v", err)
	}
	response, ok := reply.(role.ListRolesResponse)
	if !ok {
		t.Fatalf("list roles answered %T", reply)
	}
	legend := map[string][]string{}
	for _, access := range response.Roles {
		for _, action := range access.Actions {
			legend[access.Role] = append(legend[access.Role], action.Method)
		}
	}
	return legend
}
