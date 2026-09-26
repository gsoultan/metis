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
	"github.com/gsoultan/metis/server/repositories/store/organization"
	"github.com/gsoultan/storm/runtime"
)

type organizationRepository struct{ conn }

// NewOrganizationRepository returns the store of tenants.
func NewOrganizationRepository(c *db.Conn) contracts.OrganizationRepository {
	return &organizationRepository{conn{conn: c}}
}

// Get returns one organization, if it is the caller's.
func (r *organizationRepository) Get(ctx context.Context, id uuid.UUID) (models.OrganizationModel, error) {
	if err := r.requireOwnOrganization(ctx, id); err != nil {
		return models.OrganizationModel{}, err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return models.OrganizationModel{}, err
	}
	row, found, err := organization.New().Where(organization.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return models.OrganizationModel{}, fmt.Errorf("could not read the organization: %w", err)
	}
	if !found {
		return models.OrganizationModel{}, fmt.Errorf("%w: no such organization", apierr.ErrNotFound)
	}
	return organizationFrom(row), nil
}

// List returns the caller's organization, or every one for system work.
//
// A tenant sees one row, which is the one it is. Returning a list of one rather
// than the row itself is the contract the setup wizard and the organization
// picker already read, and narrowing it here would make the picker's "which
// tenant am I in" question unanswerable.
//
// System work sees every organization, not the store's first thousand; the id
// breaks ties in name so the keyset cursor is a position.
func (r *organizationRepository) List(ctx context.Context) ([]models.OrganizationModel, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}

	q := organization.New().Order(organization.Name.Asc(), organization.ID.Asc())
	if !scope.unrestricted() {
		if scope.organization == uuid.Nil {
			return nil, nil
		}
		q = q.Where(organization.ID.Eq(scope.organization))
	}
	rows, err := everyRow[organization.Row](ctx, ex, q)
	if err != nil {
		return nil, fmt.Errorf("could not list organizations: %w", err)
	}
	out := make([]models.OrganizationModel, 0, len(rows))
	for _, row := range rows {
		out = append(out, organizationFrom(row))
	}
	return out, nil
}

// Create adds an organization.
//
// Deliberately unscoped: creating a tenant is how a tenant comes to exist, so
// there is nothing to check it against. The setup wizard and the admin-gated
// endpoint are what decide who may do it.
func (r *organizationRepository) Create(ctx context.Context, o models.OrganizationModel) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	ins := organization.Create()
	if id := uuid.UUID(o.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetName(o.Name)
	setOrNullString(ins.SetDescription, ins.SetDescriptionNull, o.Description)
	if _, err := ins.Insert(ctx, ex); err != nil {
		if errors.Is(err, runtime.ErrUniqueViolation) {
			return fmt.Errorf("%w: an organization called %q already exists", apierr.ErrInvalidArgument, o.Name)
		}
		return fmt.Errorf("could not create the organization: %w", err)
	}
	return nil
}

func (r *organizationRepository) Update(ctx context.Context, o models.OrganizationModel) error {
	id := uuid.UUID(o.ID)
	if err := r.requireOwnOrganization(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	row, found, err := organization.New().Where(organization.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not read the organization: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such organization", apierr.ErrNotFound)
	}
	mut := organization.Mutate(row)
	mut.SetName(o.Name)
	setOrNullString(mut.SetDescription, mut.SetDescriptionNull, o.Description)
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not update the organization: %w", err)
	}
	return nil
}

// Delete removes an organization. Its projects go with it, by the cascade the
// model declares — leaving them would be rows scoped to a tenant that no longer
// exists, which nothing could ever read or remove.
func (r *organizationRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if err := r.requireOwnOrganization(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	if err := organization.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such organization", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not delete the organization: %w", err)
	}
	return nil
}

func organizationFrom(row organization.Row) models.OrganizationModel {
	return models.OrganizationModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		Name:        row.Name,
		Description: valueOr(row.Description),
	}
}
