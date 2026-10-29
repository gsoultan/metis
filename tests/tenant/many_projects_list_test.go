package tenant

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// An organization's project list, and an installation's organization list,
// show every one.
//
// Both read through queries the store caps at a thousand rows, in name order.
// The project picker of an organization with more than a thousand projects
// stopped at the thousandth name — so a project the organization can use
// could not be chosen — and the same held for organizations.
func TestTheProjectAndOrganizationListsShowEveryOne(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ctx, orgID := testutils.ScopedContext(t, repo)

	seedProjects(t, db, orgID, projectsAhead+1)
	projects, err := repo.Project().ListByOrganization(ctx, orgID)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != projectsAhead+1 {
		t.Errorf("the organization's project list shows %d of its %d projects", len(projects), projectsAhead+1)
	}

	seedOrganizations(t, db, projectsAhead)
	organizations, err := repo.Organization().List(entities.WithSystemContext(t.Context()))
	if err != nil {
		t.Fatalf("list organizations: %v", err)
	}
	if len(organizations) != projectsAhead+1 {
		t.Errorf("the installation's organization list shows %d of its %d organizations", len(organizations), projectsAhead+1)
	}
}
