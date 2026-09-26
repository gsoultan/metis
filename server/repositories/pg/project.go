package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/project"
	"github.com/gsoultan/storm/runtime"
)

type projectRepository struct{ conn }

// NewProjectRepository returns the store of projects.
//
// Projects are what every other scoped table is scoped *through*, so this
// repository is the one place where "which rows may this caller see" is decided
// by the organization column rather than by a project list.
func NewProjectRepository(c *db.Conn) contracts.ProjectRepository {
	return &projectRepository{conn{conn: c}}
}

func (r *projectRepository) Get(ctx context.Context, id uuid.UUID) (models.ProjectModel, error) {
	if err := r.requireProjectInTenant(ctx, id); err != nil {
		return models.ProjectModel{}, err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return models.ProjectModel{}, err
	}
	row, found, err := project.New().Where(project.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return models.ProjectModel{}, fmt.Errorf("could not read the project: %w", err)
	}
	if !found {
		return models.ProjectModel{}, fmt.Errorf("%w: no such project", apierr.ErrNotFound)
	}
	return projectFrom(row), nil
}

// List returns every project the caller may see, by name.
//
// Every one, not the store's first thousand; the id breaks ties in name so the
// keyset cursor is a position.
func (r *projectRepository) List(ctx context.Context) ([]models.ProjectModel, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}

	q := project.New().Order(project.Name.Asc(), project.ID.Asc())
	if !scope.unrestricted() {
		if scope.organization == uuid.Nil {
			return nil, nil
		}
		q = q.Where(project.OrganizationID.Eq(scope.organization))
	}
	rows, err := everyRow[project.Row](ctx, ex, q)
	if err != nil {
		return nil, fmt.Errorf("could not list projects: %w", err)
	}
	return projectsFrom(rows), nil
}

// ListByOrganization returns one organization's projects.
//
// Naming an organization that is not the caller's returns nothing rather than
// an error, which is both what the contract has always meant and the quieter
// answer: an error would distinguish "that organization is not yours" from "it
// has no projects", and the caller is not entitled to know which.
//
// No organization named means "whichever is mine", which is what the projects
// page asks for.
//
// Every project, not the store's first thousand: the picker of an organization
// with more stopped at the thousandth name.
func (r *projectRepository) ListByOrganization(ctx context.Context, organizationID uuid.UUID) ([]models.ProjectModel, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	if !scope.unrestricted() {
		if scope.organization == uuid.Nil {
			return nil, nil
		}
		if organizationID != uuid.Nil && organizationID != scope.organization {
			return nil, nil
		}
		organizationID = scope.organization
	}
	if organizationID == uuid.Nil {
		return r.List(ctx)
	}

	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := everyRow[project.Row](ctx, ex, project.New().
		Where(project.OrganizationID.Eq(organizationID)).
		Order(project.Name.Asc(), project.ID.Asc()))
	if err != nil {
		return nil, fmt.Errorf("could not list the organization's projects: %w", err)
	}
	return projectsFrom(rows), nil
}

// Create adds a project to an organization, refusing one that is not the
// caller's.
//
// This is the check that keeps the scoping honest everywhere else: every other
// table is scoped by its project, so a project planted in somebody else's
// organization would carry rows into a tenant that never asked for them.
func (r *projectRepository) Create(ctx context.Context, p models.ProjectModel) error {
	organizationID := uuid.UUID(p.OrganizationID)
	if err := r.requireOwnOrganization(ctx, organizationID); err != nil {
		return err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	ins := project.Create()
	if id := uuid.UUID(p.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetOrganizationID(organizationID)
	ins.SetName(p.Name)
	setOrNullString(ins.SetDescription, ins.SetDescriptionNull, p.Description)
	if _, err := ins.Insert(ctx, ex); err != nil {
		if errors.Is(err, runtime.ErrUniqueViolation) {
			return fmt.Errorf("%w: this organization already has a project called %q", apierr.ErrInvalidArgument, p.Name)
		}
		return fmt.Errorf("could not create the project: %w", err)
	}
	return nil
}

func (r *projectRepository) Update(ctx context.Context, p models.ProjectModel) error {
	id := uuid.UUID(p.ID)
	if err := r.requireProjectInTenant(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	row, found, err := project.New().Where(project.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the project: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such project", apierr.ErrNotFound)
	}
	mut := project.Mutate(row)
	mut.SetName(p.Name)
	setOrNullString(mut.SetDescription, mut.SetDescriptionNull, p.Description)
	// The organization is deliberately not updatable. Moving a project between
	// tenants would move every row scoped through it, silently, in one write.
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not update the project: %w", err)
	}
	return nil
}

func (r *projectRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if err := r.requireProjectInTenant(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	if err := project.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such project", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not delete the project: %w", err)
	}
	return nil
}

func projectsFrom(rows []project.Row) []models.ProjectModel {
	out := make([]models.ProjectModel, 0, len(rows))
	for _, row := range rows {
		out = append(out, projectFrom(row))
	}
	return out
}

func projectFrom(row project.Row) models.ProjectModel {
	return models.ProjectModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		OrganizationID: models.UUID(row.OrganizationID),
		Name:           row.Name,
		Description:    valueOr(row.Description),
	}
}
