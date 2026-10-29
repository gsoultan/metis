package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
	organizationstore "github.com/gsoultan/metis/server/repositories/store/organization"
	projectstore "github.com/gsoultan/metis/server/repositories/store/project"
	"github.com/gsoultan/storm/runtime/pgxdrv"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SeedEnvironmentIdentity copies the organization and project an environment
// serves into that environment's database.
//
// Not a convenience. Tenant scoping is a join: every scoped read joins
// `projects` to check the row belongs to the caller's organization, and every
// create counts the same table to check the project it names is theirs. Both
// run on the connection the rest of the query runs on, because there is no
// cross-database join. So an environment database whose `projects` table is
// empty scopes *everything* to nothing — reads come back empty and writes are
// refused with "record not found", which is what a missing row looks like from
// the outside and is nothing like what happened.
//
// Only these two rows, and only the columns scoping reads. Accounts,
// memberships and the environment registry stay in the main database, where
// MainTx sends them: they are installation-wide, and a second copy of who may
// sign in is a second answer to that question.
//
// Run whenever an environment's database is opened — at boot, and when one is
// created, enabled again or pointed at another database — because it has to be
// true of a database that was just pointed at, not only of one that existed
// when the feature was first switched on.
func SeedEnvironmentIdentity(ctx context.Context, environmentDB *pgxpool.Pool, project models.ProjectModel, organization models.OrganizationModel) error {
	ex := pgxdrv.Pool{P: environmentDB}
	// Upserts on the key that assign only what an environment is allowed to
	// learn about a tenant. A row already there keeps every column these do
	// not assign — when it was first copied, above all — and has the rest
	// refreshed, so a project renamed in the main database does not read
	// differently here. Nothing authorizes on the name; this is so anything
	// that displays one is not showing what it was called when the
	// environment was created.
	now := time.Now().UTC()
	org := organizationstore.Create()
	org.SetID(uuid.UUID(organization.ID))
	org.SetUpdatedAt(now)
	org.SetName(organization.Name)
	org.SetDescription(organization.Description)
	org.OnConflictID()
	if _, err := org.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not copy the organization into this environment: %w", err)
	}

	proj := projectstore.Create()
	proj.SetID(uuid.UUID(project.ID))
	proj.SetUpdatedAt(now)
	proj.SetOrganizationID(uuid.UUID(project.OrganizationID))
	proj.SetName(project.Name)
	proj.SetDescription(project.Description)
	proj.OnConflictID()
	if _, err := proj.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not copy the project into this environment: %w", err)
	}
	return nil
}

// identityFor reads the project an environment belongs to, and its organization.
//
// Both from the main database, under a system context: the question is which
// tenant this environment serves, so it cannot be asked from inside one.
func (a *App) identityFor(ctx context.Context, projectID uuid.UUID) (models.ProjectModel, models.OrganizationModel, error) {
	systemCtx := entities.WithSystemContext(ctx)

	project, err := a.repo.Project().Get(systemCtx, projectID)
	if err != nil {
		return models.ProjectModel{}, models.OrganizationModel{},
			fmt.Errorf("could not read the project this environment serves: %w", err)
	}
	organization, err := a.repo.Organization().Get(systemCtx, uuid.UUID(project.OrganizationID))
	if err != nil {
		return models.ProjectModel{}, models.OrganizationModel{},
			fmt.Errorf("could not read the organization this environment serves: %w", err)
	}
	return project, organization, nil
}

// errNoProject marks an environment row whose project has been deleted. Its
// database cannot be scoped to anything, so it is not served.
var errNoProject = errors.New("this environment names no project")
