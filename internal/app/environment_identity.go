package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
func SeedEnvironmentIdentity(ctx context.Context, environmentDB *gorm.DB, project models.ProjectModel, organization models.OrganizationModel) error {
	// Two independent statements rather than one shared session: a *gorm.DB
	// carries the conditions chained onto it, so reusing one for both writes
	// applies the first's Select to the second.
	//
	// Select names the columns explicitly, which both excludes the association
	// fields — GORM would otherwise try to write a ProjectModel's embedded
	// Organization as a column — and says exactly what an environment is
	// allowed to learn about a tenant.
	onConflict := clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		// Refreshed so a project renamed in the main database does not read
		// differently here. Nothing authorizes on the name; this is so anything
		// that displays one is not showing what it was called when the
		// environment was created.
		DoUpdates: clause.AssignmentColumns([]string{"name", "description", "updated_at"}),
	}

	org := models.OrganizationModel{
		Base:        models.Base{ID: organization.ID},
		Name:        organization.Name,
		Description: organization.Description,
	}
	if err := environmentDB.WithContext(ctx).
		Clauses(onConflict).
		Select("id", "created_at", "updated_at", "name", "description").
		Create(&org).Error; err != nil {
		return fmt.Errorf("could not copy the organization into this environment: %w", err)
	}

	proj := models.ProjectModel{
		Base:           models.Base{ID: project.ID},
		OrganizationID: project.OrganizationID,
		Name:           project.Name,
		Description:    project.Description,
	}
	if err := environmentDB.WithContext(ctx).
		Clauses(onConflict).
		Select("id", "created_at", "updated_at", "organization_id", "name", "description").
		Create(&proj).Error; err != nil {
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
