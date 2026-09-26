package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	storegroup "github.com/gsoultan/metis/server/repositories/store/group"
	"github.com/gsoultan/metis/server/repositories/store/membership"
	"github.com/gsoultan/metis/server/repositories/store/user"
	"github.com/gsoultan/storm/runtime"
)

type groupRepository struct{ conn }

// NewGroupRepository returns the named sets of people inside an organization.
//
// Distinct from a project's participant groups: this one grants what its
// members may do on the platform.
func NewGroupRepository(c *db.Conn) contracts.GroupRepository {
	return &groupRepository{conn{conn: c}}
}

// List returns an organization's groups.
//
// Scoped, and it was not: this returned every group in the installation with
// its memberships preloaded.
//
// Every group, not the store's first thousand; the id breaks ties in name so
// the keyset cursor is a position.
func (r *groupRepository) List(ctx context.Context, organizationID uuid.UUID) ([]models.GroupModel, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	if !scope.unrestricted() {
		switch {
		case scope.organization == uuid.Nil:
			return nil, nil
		case organizationID != uuid.Nil && organizationID != scope.organization:
			return nil, nil
		default:
			organizationID = scope.organization
		}
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	q := storegroup.New().Order(storegroup.Name.Asc(), storegroup.ID.Asc())
	if organizationID != uuid.Nil {
		q = q.Where(storegroup.OrganizationID.Eq(organizationID))
	}
	rows, err := everyRow[storegroup.Row](ctx, ex, q)
	if err != nil {
		return nil, fmt.Errorf("could not list groups: %w", err)
	}
	out := make([]models.GroupModel, 0, len(rows))
	for _, row := range rows {
		g, err := platformGroupFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

func (r *groupRepository) Get(ctx context.Context, id uuid.UUID) (models.GroupModel, error) {
	row, err := r.row(ctx, id)
	if err != nil {
		return models.GroupModel{}, err
	}
	return platformGroupFrom(row)
}

func (r *groupRepository) Create(ctx context.Context, g models.GroupModel) error {
	organizationID := uuid.UUID(g.OrganizationID)
	if err := r.requireOwnOrganization(ctx, organizationID); err != nil {
		return err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	roles, err := json.Marshal(g.Roles)
	if err != nil {
		return fmt.Errorf("could not encode the group's roles: %w", err)
	}
	ins := storegroup.Create()
	if id := uuid.UUID(g.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetOrganizationID(organizationID)
	ins.SetName(g.Name)
	ins.SetDescription(g.Description)
	ins.SetRoles(roles)
	if _, err := ins.Insert(ctx, ex); err != nil {
		return fmt.Errorf("could not create the group: %w", err)
	}
	return nil
}

func (r *groupRepository) Update(ctx context.Context, g models.GroupModel) error {
	row, err := r.row(ctx, uuid.UUID(g.ID))
	if err != nil {
		return err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	roles, err := json.Marshal(g.Roles)
	if err != nil {
		return fmt.Errorf("could not encode the group's roles: %w", err)
	}
	mut := storegroup.Mutate(row)
	mut.SetName(g.Name)
	mut.SetDescription(g.Description)
	mut.SetRoles(roles)
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not update the group: %w", err)
	}
	return nil
}

func (r *groupRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.row(ctx, id); err != nil {
		return err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	if err := storegroup.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such group", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not delete the group: %w", err)
	}
	return nil
}

// ListGroupMembers returns who is in a group.
//
// Everybody, not the store's first thousand: the memberships and then the
// accounts are both read through pg.everyRow.
func (r *groupRepository) ListGroupMembers(ctx context.Context, groupID uuid.UUID) ([]models.UserModel, error) {
	if !r.canSeeGroup(ctx, groupID) {
		return nil, nil
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := everyRow[membership.Row](ctx, ex, membership.New().
		Where(membership.GroupID.Eq(groupID)))
	if err != nil {
		return nil, fmt.Errorf("could not read the group's members: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([][16]byte, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	members, err := everyRow[user.Row](ctx, ex, user.New().
		Where(user.ID.In(ids...)).
		Order(user.Username.Asc(), user.ID.Asc()))
	if err != nil {
		return nil, fmt.Errorf("could not read the group's members: %w", err)
	}
	out := make([]models.UserModel, 0, len(members))
	for _, row := range members {
		out = append(out, models.UserModel{
			Base: models.Base{
				ID:        models.UUID(row.ID),
				CreatedAt: row.CreatedAt,
				UpdatedAt: row.UpdatedAt,
			},
			Username:     row.Username,
			FullName:     row.FullName,
			DisplayName:  row.DisplayName,
			Organization: row.Organization,
			Email:        row.Email,
		})
	}
	return out, nil
}

// AddMembership puts an account in a group, and says nothing if it is already
// there — the same act twice is the same state.
func (r *groupRepository) AddMembership(ctx context.Context, userID, groupID uuid.UUID) error {
	if _, err := r.row(ctx, groupID); err != nil {
		return err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	ins := membership.Create()
	ins.SetUserID(userID)
	ins.SetGroupID(groupID)
	ins.DoNothing()
	if _, err := ins.Insert(ctx, ex); err != nil && !errors.Is(err, runtime.ErrConflict) {
		return fmt.Errorf("could not add the account to the group: %w", err)
	}
	return nil
}

func (r *groupRepository) RemoveMembership(ctx context.Context, userID, groupID uuid.UUID) error {
	if _, err := r.row(ctx, groupID); err != nil {
		return err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	if err := membership.Delete(ctx, ex, userID, groupID); err != nil && !errors.Is(err, runtime.ErrNoRow) {
		return fmt.Errorf("could not remove the account from the group: %w", err)
	}
	return nil
}

// ListUserGroups returns the groups one account is in.
func (r *groupRepository) ListUserGroups(ctx context.Context, userID uuid.UUID) ([]models.GroupModel, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := membership.New().
		Where(membership.UserID.Eq(userID)).
		Unordered().
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the account's groups: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([][16]byte, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.GroupID)
	}

	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, err
	}
	q := storegroup.New().Where(storegroup.ID.In(ids...)).Order(storegroup.Name.Asc())
	if !scope.unrestricted() {
		if scope.organization == uuid.Nil {
			return nil, nil
		}
		q = q.Where(storegroup.OrganizationID.Eq(scope.organization))
	}
	groups, err := q.All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the account's groups: %w", err)
	}
	out := make([]models.GroupModel, 0, len(groups))
	for _, row := range groups {
		g, err := platformGroupFrom(row)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

// canSeeGroup answers the visibility question as a boolean, for the list that
// turns "not yours" into no rows rather than an error.
func (r *groupRepository) canSeeGroup(ctx context.Context, id uuid.UUID) bool {
	_, err := r.row(ctx, id)
	return err == nil
}

func (r *groupRepository) row(ctx context.Context, id uuid.UUID) (storegroup.Row, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return storegroup.Row{}, err
	}
	row, found, err := storegroup.New().Where(storegroup.ID.Eq(id)).One(ctx, ex)
	if err != nil {
		return storegroup.Row{}, fmt.Errorf("could not read the group: %w", err)
	}
	if !found {
		return storegroup.Row{}, fmt.Errorf("%w: no such group", apierr.ErrNotFound)
	}
	if err := r.requireOwnOrganization(ctx, uuid.UUID(row.OrganizationID)); err != nil {
		return storegroup.Row{}, fmt.Errorf("%w: no such group", apierr.ErrNotFound)
	}
	return row, nil
}

func platformGroupFrom(row storegroup.Row) (models.GroupModel, error) {
	g := models.GroupModel{
		Base: models.Base{
			ID:        models.UUID(row.ID),
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		OrganizationID: models.UUID(row.OrganizationID),
		Name:           row.Name,
		Description:    row.Description,
	}
	if len(row.Roles) > 0 {
		if err := json.Unmarshal(row.Roles, &g.Roles); err != nil {
			return models.GroupModel{}, fmt.Errorf("could not decode a group's roles: %w", err)
		}
	}
	return g, nil
}
