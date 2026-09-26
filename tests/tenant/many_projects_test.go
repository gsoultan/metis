package tenant

import (
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
)

// projectsAhead is as many projects as one read of the generated store
// returns: storm starts every query with a limit of 1000.
const projectsAhead = 1000

// A request is scoped to every project its organization has.
//
// The scope is the list of the organization's project ids, read on every
// scoped call through a query the store caps at a thousand rows. In an
// organization with more, the projects past the first thousand were not in
// it: everything inside them read as not found and every write into them was
// refused, as though they belonged to another organization.
func TestAnOrganizationReachesEveryProjectPastTheFirstThousand(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	ctx, orgID := testutils.ScopedContext(t, repo)

	if err := db.WithContext(ctx).Exec(`
		INSERT INTO projects (id, created_at, updated_at, organization_id, name)
		SELECT gen_random_uuid(), now(), now(), ?, format('project-%s', lpad(n::text, 5, '0'))
		  FROM generate_series(1, ?) AS n`, orgID, projectsAhead).Error; err != nil {
		t.Fatalf("seed %d projects: %v", projectsAhead, err)
	}
	late := uuid.Must(uuid.NewV7())
	if err := repo.Project().Create(entities.WithSystemContext(ctx), models.ProjectModel{
		Base:           models.Base{ID: models.UUID(late)},
		OrganizationID: models.UUID(orgID),
		Name:           "The project after a thousand",
	}); err != nil {
		t.Fatalf("create the project after a thousand: %v", err)
	}

	if _, err := repo.Project().Get(ctx, late); err != nil {
		t.Fatalf("the organization's project created after %d others cannot be read by the organization: %v",
			projectsAhead, err)
	}
	if err := repo.Definition().Create(ctx, models.ProcessDefinitionModel{
		ProjectID: models.UUID(late),
		Key:       "intake",
		Name:      "Intake",
		Version:   1,
	}); err != nil {
		t.Fatalf("deploying into the organization's project created after %d others was refused: %v",
			projectsAhead, err)
	}
}
