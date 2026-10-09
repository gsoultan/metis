package testutils

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
)

// EnrolAdministrator writes the account a test's signed-in administrator
// stands for: an account with that id and name that belongs to organization
// and holds the administrator role on the account.
//
// A test that signs somebody in by putting a principal in the context has a
// caller the services accept, and no account behind it. What asks about an
// account by its id — an approval asks whether whoever made the request still
// administers the organization — then finds nobody, which is a state
// production never has: a request is made by an account. So the helpers that
// build such principals write the account first, once for each harness.
//
// The account is written through the repository, with a password hash that
// matches no password: nothing here signs in with it. An account that is
// already there is left as it is, so a harness may be asked twice.
func EnrolAdministrator(ctx context.Context, repo repositories.Repository, organization, id uuid.UUID, name string) error {
	if _, err := repo.User().Get(entities.WithSystemContext(ctx), id); err == nil {
		return nil
	}
	account := models.UserModel{
		Base:          models.Base{ID: models.UUID(id)},
		Username:      name,
		Roles:         []string{entities.RoleAdmin},
		Organizations: []models.OrganizationModel{{Base: models.Base{ID: models.UUID(organization)}}},
	}
	if err := repo.User().Create(entities.WithSystemContext(ctx), account, "no password matches this"); err != nil {
		return fmt.Errorf("enrol the administrator %s: %w", name, err)
	}
	return nil
}
