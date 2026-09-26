package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/platformrole"
	"github.com/gsoultan/metis/server/repositories/store/platformroleassignment"
	"github.com/gsoultan/metis/server/repositories/store/platformuser"
	"github.com/gsoultan/storm/runtime"
)

type platformUserRepository struct {
	conn *db.Conn
}

// NewPlatformUserRepository returns the store of accounts that administer
// Metis.
func NewPlatformUserRepository(conn *db.Conn) contracts.PlatformUserRepository {
	return &platformUserRepository{conn: conn}
}

// List returns every platform account with its roles.
//
// Every one, not the store's first thousand, and with every grant: both reads
// go through pg.everyRow. The id breaks ties in username so the keyset cursor
// is a position.
func (r *platformUserRepository) List(ctx context.Context) ([]entities.PlatformUser, error) {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := everyRow[platformuser.Row](ctx, ex, platformuser.New().
		Order(platformuser.Username.Asc(), platformuser.ID.Asc()))
	if err != nil {
		return nil, fmt.Errorf("could not list platform accounts: %w", err)
	}

	// Grants for everybody in one read rather than one per account: this is the
	// page an administrator opens, and a query per row is how a list of twenty
	// becomes twenty-one round trips.
	grants, err := r.grantsByUser(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]entities.PlatformUser, 0, len(rows))
	for _, row := range rows {
		account := platformUserFrom(row)
		account.Roles = grants[row.ID]
		out = append(out, account)
	}
	return out, nil
}

func (r *platformUserRepository) Get(ctx context.Context, id uuid.UUID) (entities.PlatformUser, error) {
	row, err := r.readAccount(ctx, id)
	if err != nil {
		return entities.PlatformUser{}, err
	}
	roles, err := r.rolesOf(ctx, id)
	if err != nil {
		return entities.PlatformUser{}, err
	}
	account := platformUserFrom(row)
	account.Roles = roles
	return account, nil
}

func (r *platformUserRepository) Create(ctx context.Context, account entities.PlatformUser, passwordHash string) (uuid.UUID, error) {
	if passwordHash == "" {
		// Refused rather than defaulted: an administrator account with no
		// credentials is one anybody could later claim by setting a password.
		return uuid.Nil, apierr.Invalidf("a platform account needs a password")
	}

	var id uuid.UUID
	err := r.conn.Transact(ctx, func(txCtx context.Context) error {
		ex, err := r.conn.Executor(txCtx)
		if err != nil {
			return err
		}
		ins := platformuser.Create()
		ins.SetUsername(account.Username)
		ins.SetPasswordHash(passwordHash)
		setOrNullString(ins.SetFullName, ins.SetFullNameNull, account.FullName)
		setOrNullString(ins.SetDisplayName, ins.SetDisplayNameNull, account.DisplayName)
		setOrNullString(ins.SetEmail, ins.SetEmailNull, account.Email)
		row, err := ins.Insert(txCtx, ex)
		if err != nil {
			return fmt.Errorf("could not create the account: %w", err)
		}
		id = row.ID
		// The account and its grants go in together: an account that exists
		// with no roles is one somebody has to notice and fix.
		return r.setRoles(txCtx, id, account.Roles)
	})
	return id, err
}

func (r *platformUserRepository) Update(ctx context.Context, account entities.PlatformUser) error {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return err
	}
	row, err := r.readAccount(ctx, account.ID)
	if err != nil {
		return err
	}
	mut := platformuser.Mutate(row)
	mut.SetUsername(account.Username)
	setOrNullString(mut.SetFullName, mut.SetFullNameNull, account.FullName)
	setOrNullString(mut.SetDisplayName, mut.SetDisplayNameNull, account.DisplayName)
	setOrNullString(mut.SetEmail, mut.SetEmailNull, account.Email)
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not update the account: %w", err)
	}
	return nil
}

// Delete removes an account, refusing to remove the last administrator.
//
// An installation with nobody who can administer it cannot be repaired from
// inside it: there is no screen left that would let somebody grant the role
// back. The check and the delete share a transaction so two administrators
// deleting each other at the same moment cannot both pass it.
func (r *platformUserRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.conn.Transact(ctx, func(txCtx context.Context) error {
		roles, err := r.rolesOf(txCtx, id)
		if err != nil {
			return err
		}
		if entities.HasRole(roles, entities.RoleAdmin) {
			remaining, err := r.CountAdministrators(txCtx)
			if err != nil {
				return err
			}
			if remaining <= 1 {
				return apierr.Invalidf("this is the last administrator; grant the role to somebody else first")
			}
		}

		ex, err := r.conn.Executor(txCtx)
		if err != nil {
			return err
		}
		if err := platformuser.Delete(txCtx, ex, id); err != nil {
			if errors.Is(err, runtime.ErrNoRow) {
				return fmt.Errorf("%w: no such platform account", apierr.ErrNotFound)
			}
			return fmt.Errorf("could not delete the account: %w", err)
		}
		return nil
	})
}

// SetRoles replaces an account's grants, refusing to remove the last
// administrator's own admin role.
func (r *platformUserRepository) SetRoles(ctx context.Context, id uuid.UUID, roles []string) error {
	return r.conn.Transact(ctx, func(txCtx context.Context) error {
		had, err := r.rolesOf(txCtx, id)
		if err != nil {
			return err
		}
		losingAdmin := entities.HasRole(had, entities.RoleAdmin) && !entities.HasRole(roles, entities.RoleAdmin)
		if losingAdmin {
			remaining, err := r.CountAdministrators(txCtx)
			if err != nil {
				return err
			}
			if remaining <= 1 {
				return apierr.Invalidf("this is the last administrator; grant the role to somebody else first")
			}
		}
		return r.setRoles(txCtx, id, roles)
	})
}

// setRoles rewrites the grants, without the last-administrator check.
//
// Replace rather than diff: the caller sends the set they want, and computing a
// minimal difference would be more code for the same result on a table with a
// handful of rows per account.
func (r *platformUserRepository) setRoles(ctx context.Context, id uuid.UUID, roles []string) error {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return err
	}

	known, err := r.rolesByName(ctx)
	if err != nil {
		return err
	}
	wanted := make(map[uuid.UUID]struct{}, len(roles))
	for _, name := range roles {
		roleID, ok := known[name]
		if !ok {
			// Refused rather than ignored: a typo that silently grants nothing
			// is an account somebody believes is an administrator and is not.
			// Refused before anything is written, so a bad name in the middle
			// of a list does not leave the first half applied.
			return apierr.Invalidf("%q is not a role", name)
		}
		wanted[roleID] = struct{}{}
	}

	// Revoke first, then grant. storm deletes by primary key rather than by
	// predicate, which suits a table whose key is the grant itself: each
	// revocation names the exact pair being taken away.
	held, err := platformroleassignment.New().
		Where(platformroleassignment.PlatformUserID.Eq(id)).
		All(ctx, ex, nil)
	if err != nil {
		return fmt.Errorf("could not read the account's roles: %w", err)
	}
	for _, row := range held {
		if _, keep := wanted[uuid.UUID(row.PlatformRoleID)]; keep {
			// Already granted. Deleting and re-inserting it would be the same
			// state reached through a moment where the account has no role.
			delete(wanted, uuid.UUID(row.PlatformRoleID))
			continue
		}
		if err := platformroleassignment.Delete(ctx, ex, row.PlatformUserID, row.PlatformRoleID); err != nil {
			return fmt.Errorf("could not revoke a role: %w", err)
		}
	}

	for roleID := range wanted {
		ins := platformroleassignment.Create()
		ins.SetPlatformUserID(id)
		ins.SetPlatformRoleID(roleID)
		ins.OnConflictPlatformUserIDPlatformRoleID().DoNothing()
		if _, err := ins.Insert(ctx, ex); err != nil && !errors.Is(err, runtime.ErrConflict) {
			return fmt.Errorf("could not grant a role: %w", err)
		}
	}
	return nil
}

func (r *platformUserRepository) ListRoles(ctx context.Context) ([]entities.PlatformRole, error) {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := platformrole.New().
		Order(platformrole.Name.Asc()).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not list platform roles: %w", err)
	}
	out := make([]entities.PlatformRole, 0, len(rows))
	for _, row := range rows {
		out = append(out, entities.PlatformRole{
			ID:          row.ID,
			Name:        row.Name,
			Description: valueOr(row.Description),
			BuiltIn:     row.BuiltIn,
		})
	}
	return out, nil
}

// EnsureBuiltInRoles creates the roles the installation ships with.
//
// Idempotent and called at boot, so an installation upgraded into this feature
// has its roles without anybody running anything.
func (r *platformUserRepository) EnsureBuiltInRoles(ctx context.Context) error {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return err
	}
	for _, role := range entities.BuiltInPlatformRoles() {
		ins := platformrole.Create()
		ins.SetName(role.Name)
		ins.SetDescription(role.Description)
		ins.SetBuiltIn(true)
		// An empty array, not nil. The column is NOT NULL and a nil
		// runtime.JSON is SQL NULL, so seeding the built-in roles failed at
		// boot with a constraint violation — on a fresh installation, before
		// anybody could sign in to read it.
		ins.SetPermissions(runtime.JSON("[]"))
		// Restore rather than DO NOTHING. The name is unique across the deleted
		// rows, so a built-in role that somehow got marked would keep taking its
		// name and DO NOTHING would leave it marked — an installation whose
		// ADMIN role exists, is invisible, and cannot be recreated. Clearing
		// deleted_at makes "the built-in roles exist" true at every boot, which
		// is what this function claims.
		ins.SetDeletedAtNull()
		ins.OnConflictName()
		if _, err := ins.Insert(ctx, ex); err != nil && !errors.Is(err, runtime.ErrConflict) {
			return fmt.Errorf("could not create the %s role: %w", role.Name, err)
		}
	}
	return nil
}

// liveGrantCount counts one role's grants held by accounts that have not been
// deleted. Raw SQL for the join: the generated store counts one table.
const liveGrantCount = `SELECT count(*)
	  FROM platform_role_assignments a
	  JOIN platform_users u ON u.id = a.platform_user_id
	 WHERE a.platform_role_id = $1
	   AND u.deleted_at IS NULL`

// CountAdministrators reports how many accounts still hold the admin role.
//
// Accounts that exist, not grants. Deleting an account marks its row and
// leaves its grants in place, so counting grants counted a deleted
// administrator as a live one: after one of two was deleted the other still
// counted two, and could be deleted or demoted in turn.
func (r *platformUserRepository) CountAdministrators(ctx context.Context) (int, error) {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return 0, err
	}
	known, err := r.rolesByName(ctx)
	if err != nil {
		return 0, err
	}
	adminID, ok := known[entities.RoleAdmin]
	if !ok {
		return 0, nil
	}
	rows, err := ex.Query(ctx, liveGrantCount, []any{adminID})
	if err != nil {
		return 0, fmt.Errorf("could not count administrators: %w", err)
	}
	defer rows.Close()
	var count int64
	if rows.Next() {
		if err := scanInt(rows.RawValues(), &count); err != nil {
			return 0, err
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("could not count administrators: %w", err)
	}
	return int(count), nil
}

func (r *platformUserRepository) readAccount(ctx context.Context, id uuid.UUID) (platformuser.Row, error) {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return platformuser.Row{}, err
	}
	row, found, err := platformuser.New().
		Where(platformuser.ID.Eq(id)).
		One(ctx, ex)
	if err != nil {
		return platformuser.Row{}, fmt.Errorf("could not read the account: %w", err)
	}
	if !found {
		return platformuser.Row{}, fmt.Errorf("%w: no such platform account", apierr.ErrNotFound)
	}
	return row, nil
}

// rolesOf reads the grants of one account.
//
// Only that account's. It read every grant in the installation and picked this
// account's out, through a query the store caps at a thousand rows: past a
// thousand grants an administrator's own could fall outside the window, the
// account read as holding no role, and the last-administrator checks in Delete
// and SetRoles — which run only for an account holding the role — let the last
// one go.
func (r *platformUserRepository) rolesOf(ctx context.Context, id uuid.UUID) ([]string, error) {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	nameByID, err := roleNamesByID(ctx, ex)
	if err != nil {
		return nil, err
	}
	assignments, err := platformroleassignment.New().
		Where(platformroleassignment.PlatformUserID.Eq(id)).
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the account's roles: %w", err)
	}
	var roles []string
	for _, row := range assignments {
		if name, ok := nameByID[row.PlatformRoleID]; ok {
			roles = append(roles, name)
		}
	}
	return roles, nil
}

// roleNamesByID reads the role list, which is a handful of rows.
func roleNamesByID(ctx context.Context, ex runtime.Executor) (map[uuid.UUID]string, error) {
	roleRows, err := platformrole.New().All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read roles: %w", err)
	}
	nameByID := make(map[uuid.UUID]string, len(roleRows))
	for _, row := range roleRows {
		nameByID[row.ID] = row.Name
	}
	return nameByID, nil
}

// grantsByUser reads every grant and indexes it by account.
//
// Two reads for the whole picture rather than a join: the tables are small, the
// role list is a handful of rows, and a join would return one row per grant
// which then has to be regrouped anyway.
func (r *platformUserRepository) grantsByUser(ctx context.Context) (map[uuid.UUID][]string, error) {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	nameByID, err := roleNamesByID(ctx, ex)
	if err != nil {
		return nil, err
	}

	assignments, err := everyRow[platformroleassignment.Row](ctx, ex, platformroleassignment.New())
	if err != nil {
		return nil, fmt.Errorf("could not read role assignments: %w", err)
	}
	grants := map[uuid.UUID][]string{}
	for _, row := range assignments {
		if name, ok := nameByID[row.PlatformRoleID]; ok {
			grants[row.PlatformUserID] = append(grants[row.PlatformUserID], name)
		}
	}
	return grants, nil
}

func (r *platformUserRepository) rolesByName(ctx context.Context) (map[string]uuid.UUID, error) {
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := platformrole.New().All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read roles: %w", err)
	}
	byName := make(map[string]uuid.UUID, len(rows))
	for _, row := range rows {
		byName[row.Name] = row.ID
	}
	return byName, nil
}

func platformUserFrom(row platformuser.Row) entities.PlatformUser {
	return entities.PlatformUser{
		ID:          row.ID,
		Username:    row.Username,
		FullName:    valueOr(row.FullName),
		DisplayName: valueOr(row.DisplayName),
		Email:       valueOr(row.Email),
		CreatedAt:   row.CreatedAt,
	}
}
