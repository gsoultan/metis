package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/store/user"
	"github.com/gsoultan/metis/server/repositories/store/userorganization"
	"github.com/gsoultan/metis/server/repositories/store/userproject"
	"github.com/gsoultan/storm/runtime"
)

type userRepository struct{ conn }

// NewUserRepository returns the accounts that can sign in.
//
// Installation-wide rather than tenant-scoped where identity is concerned: an
// account is resolved before there is a tenant to scope by, which is what makes
// the lookup by username the one read that cannot be filtered. Listing accounts
// is scoped, because that is a directory rather than an authentication.
func NewUserRepository(c *db.Conn) contracts.UserRepository {
	return &userRepository{conn{conn: c}}
}

func (r *userRepository) Get(ctx context.Context, id uuid.UUID) (models.UserModel, error) {
	row, err := r.row(ctx, user.ID.Eq(id))
	if err != nil {
		return models.UserModel{}, err
	}
	return r.hydrate(ctx, row)
}

// GetByUsername resolves an account by the name somebody typed.
//
// Unscoped, necessarily: this runs during login, before any tenant exists to
// scope by. It is also why the username is unique across the deleted rows —
// reissuing one would let a new person inherit an old one's audit trail.
func (r *userRepository) GetByUsername(ctx context.Context, username string) (models.UserModel, error) {
	row, err := r.row(ctx, user.Username.Eq(username))
	if err != nil {
		return models.UserModel{}, err
	}
	return r.hydrate(ctx, row)
}

// GetWithPasswordByUsername returns the account and its hash.
//
// The hash is a separate return rather than a field, so it cannot ride along on
// a struct that is otherwise handed to callers and serialised.
func (r *userRepository) GetWithPasswordByUsername(ctx context.Context, username string) (models.UserModel, string, error) {
	row, err := r.row(ctx, user.Username.Eq(username))
	if err != nil {
		return models.UserModel{}, "", err
	}
	user, err := r.hydrate(ctx, row)
	if err != nil {
		return models.UserModel{}, "", err
	}
	return user, row.PasswordHash, nil
}

func (r *userRepository) GetWithPasswordByID(ctx context.Context, id uuid.UUID) (models.UserModel, string, error) {
	row, err := r.row(ctx, user.ID.Eq(id))
	if err != nil {
		return models.UserModel{}, "", err
	}
	user, err := r.hydrate(ctx, row)
	if err != nil {
		return models.UserModel{}, "", err
	}
	return user, row.PasswordHash, nil
}

// ListByOrganization returns the accounts in one tenant.
//
// Scoped, and it was not: this returned every account in the installation with
// their memberships preloaded — a complete staff directory of every tenant, to
// anybody signed in.
//
// Every member, not the store's first thousand: the memberships and the
// accounts are both read through pg.everyRow, and the memberships of the
// accounts listed come in two reads for the whole list rather than two per
// account.
func (r *userRepository) ListByOrganization(ctx context.Context, organizationID uuid.UUID) ([]models.UserModel, error) {
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

	// The id breaks ties so the cursor is a position; a username is unique,
	// so it never has to.
	q := user.New().Order(user.Username.Asc(), user.ID.Asc())
	if organizationID != uuid.Nil {
		members, err := everyRow[userorganization.Row](ctx, ex, userorganization.New().
			Where(userorganization.OrganizationID.Eq(organizationID)))
		if err != nil {
			return nil, fmt.Errorf("could not read the organization's members: %w", err)
		}
		if len(members) == 0 {
			return nil, nil
		}
		ids := make([][16]byte, 0, len(members))
		for _, member := range members {
			ids = append(ids, member.UserID)
		}
		q = q.Where(user.ID.In(ids...))
	}
	rows, err := everyRow[user.Row](ctx, ex, q)
	if err != nil {
		return nil, fmt.Errorf("could not list accounts: %w", err)
	}
	return hydrateAll(ctx, ex, rows)
}

// hydrateAll loads the memberships of many accounts: two reads for the list,
// where hydrate is two per account.
func hydrateAll(ctx context.Context, ex runtime.Executor, rows []user.Row) ([]models.UserModel, error) {
	out := make([]models.UserModel, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([][16]byte, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	orgs, err := everyRow[userorganization.Row](ctx, ex, userorganization.New().Where(userorganization.UserID.In(ids...)))
	if err != nil {
		return nil, fmt.Errorf("could not read the accounts' organizations: %w", err)
	}
	projects, err := everyRow[userproject.Row](ctx, ex, userproject.New().Where(userproject.UserID.In(ids...)))
	if err != nil {
		return nil, fmt.Errorf("could not read the accounts' projects: %w", err)
	}
	orgsOf := make(map[[16]byte][]models.OrganizationModel, len(rows))
	for _, org := range orgs {
		orgsOf[org.UserID] = append(orgsOf[org.UserID], models.OrganizationModel{Base: models.Base{ID: models.UUID(org.OrganizationID)}})
	}
	projectsOf := make(map[[16]byte][]models.ProjectModel, len(rows))
	for _, project := range projects {
		projectsOf[project.UserID] = append(projectsOf[project.UserID], models.ProjectModel{Base: models.Base{ID: models.UUID(project.ProjectID)}})
	}
	for _, row := range rows {
		account, err := accountFrom(row)
		if err != nil {
			return nil, err
		}
		account.Organizations = orgsOf[row.ID]
		account.Projects = projectsOf[row.ID]
		out = append(out, account)
	}
	return out, nil
}

// otherAdministratorCandidates reads the roles of an organization's members,
// other than one account, whose roles could name the administrator role.
//
// A narrowing, not the decision: HasAnotherAdministrator decides with
// entities.HasRole, the same test that grants an administrator their access.
// translate() folds exactly the letters of ADMIN, as HasRole folds ASCII;
// lower() would follow the database's locale, and a Turkish one lowers 'I' to
// a dotless 'ı' and would hide every administrator. The quotes keep
// "ADMINISTRATOR" and the like out.
const otherAdministratorCandidates = `SELECT u.roles::text
	  FROM users u
	  JOIN user_organizations m ON m.user_id = u.id
	 WHERE m.organization_id = $1
	   AND u.id <> $2
	   AND u.deleted_at IS NULL
	   AND translate(u.roles::text, 'ADMIN', 'admin') LIKE '%"admin"%'`

// HasAnotherAdministrator reports whether an organization has an administrator
// besides one account.
//
// Asked of the database rather than of a list of the members. The list stops
// at the store's thousand rows, so in a larger organization the other
// administrator could be past the end of it, and removing one of two
// administrators was refused as though it removed the last.
//
// An organization that is not the caller's has no members they can see, so
// nobody in it counts — and the guard this serves refuses rather than allows.
func (r *userRepository) HasAnotherAdministrator(ctx context.Context, organizationID, except uuid.UUID) (bool, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return false, err
	}
	if !scope.unrestricted() && scope.organization != organizationID {
		return false, nil
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return false, err
	}
	rows, err := ex.Query(ctx, otherAdministratorCandidates, []any{organizationID, except})
	if err != nil {
		return false, fmt.Errorf("could not look for another administrator: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var roles []string
		if err := json.Unmarshal(rows.RawValues()[0], &roles); err != nil {
			return false, fmt.Errorf("could not decode an account's roles: %w", err)
		}
		if entities.HasRole(roles, entities.RoleAdmin) {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("could not look for another administrator: %w", err)
	}
	return false, nil
}

// HasAccounts reports whether any account exists, deleted or not.
//
// Deleted ones count: a database somebody has signed in to is an installation
// even after every account in it is gone, and setup must not hand it to the
// next visitor as new. Raw SQL for that reason — the generated Exists keeps
// deleted rows out of every read, which is right everywhere except here.
func (r *userRepository) HasAccounts(ctx context.Context) (bool, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return false, err
	}
	rows, err := ex.Query(ctx, `SELECT 1 FROM users LIMIT 1`, nil)
	if err != nil {
		return false, fmt.Errorf("could not tell whether any account exists: %w", err)
	}
	defer rows.Close()
	return rows.Next(), rows.Err()
}

func (r *userRepository) Create(ctx context.Context, u models.UserModel, passwordHash string) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	roles, err := json.Marshal(u.Roles)
	if err != nil {
		return fmt.Errorf("could not encode the account's roles: %w", err)
	}
	ins := user.Create()
	if id := uuid.UUID(u.ID); id != uuid.Nil {
		ins.SetID(id)
	}
	ins.SetUsername(u.Username)
	ins.SetPasswordHash(passwordHash)
	ins.SetFullName(u.FullName)
	ins.SetDisplayName(u.DisplayName)
	ins.SetOrganization(u.Organization)
	ins.SetEmail(u.Email)
	ins.SetRoles(roles)
	row, err := ins.Insert(ctx, ex)
	if err != nil {
		if errors.Is(err, runtime.ErrUniqueViolation) {
			return fmt.Errorf("%w: an account called %q already exists", apierr.ErrInvalidArgument, u.Username)
		}
		return fmt.Errorf("could not create the account: %w", err)
	}
	for _, org := range u.Organizations {
		if err := r.AddOrganization(ctx, row.ID, uuid.UUID(org.ID)); err != nil {
			return err
		}
	}
	for _, project := range u.Projects {
		if err := r.AddProject(ctx, row.ID, uuid.UUID(project.ID)); err != nil {
			return err
		}
	}
	return nil
}

// SetPasswordHash replaces the credential and ends every session issued before
// it — which is the whole reason somebody changes a password they think has
// been compromised.
func (r *userRepository) SetPasswordHash(ctx context.Context, id uuid.UUID, passwordHash string) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	row, err := r.row(ctx, user.ID.Eq(id))
	if err != nil {
		return err
	}
	mut := user.Mutate(row)
	mut.SetPasswordHash(passwordHash)
	mut.SetTokensValidFrom(nowUTC())
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not set the password: %w", err)
	}
	return nil
}

// SetProfile writes how an account is named and reached, and nothing else.
//
// The UPDATE names three columns. Update would write every one from what its
// caller read — so an owner renaming themselves at the moment an administrator
// took a role away would write the old roles back, and undo the demotion.
func (r *userRepository) SetProfile(ctx context.Context, u models.UserModel) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	row, err := r.row(ctx, user.ID.Eq(uuid.UUID(u.ID)))
	if err != nil {
		return err
	}
	mut := user.Mutate(row)
	mut.SetFullName(u.FullName)
	mut.SetDisplayName(u.DisplayName)
	mut.SetEmail(u.Email)
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not save the profile: %w", err)
	}
	return nil
}

// Update saves an account's profile.
//
// The password hash is deliberately not written here. It has its own method, so
// a caller that read an account, changed a name and wrote it back cannot blank
// the credential with a field it never populated.
func (r *userRepository) Update(ctx context.Context, u models.UserModel) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	row, err := r.row(ctx, user.ID.Eq(uuid.UUID(u.ID)))
	if err != nil {
		return err
	}
	roles, err := json.Marshal(u.Roles)
	if err != nil {
		return fmt.Errorf("could not encode the account's roles: %w", err)
	}
	mut := user.Mutate(row)
	mut.SetUsername(u.Username)
	mut.SetFullName(u.FullName)
	mut.SetDisplayName(u.DisplayName)
	mut.SetOrganization(u.Organization)
	mut.SetEmail(u.Email)
	mut.SetRoles(roles)
	if err := mut.Update(ctx, ex); err != nil {
		return fmt.Errorf("could not update the account: %w", err)
	}
	return nil
}

func (r *userRepository) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := r.row(ctx, user.ID.Eq(id)); err != nil {
		return err
	}
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	if err := user.Delete(ctx, ex, id); err != nil {
		if errors.Is(err, runtime.ErrNoRow) {
			return fmt.Errorf("%w: no such account", apierr.ErrNotFound)
		}
		return fmt.Errorf("could not delete the account: %w", err)
	}
	return nil
}

func (r *userRepository) AddOrganization(ctx context.Context, userID, organizationID uuid.UUID) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	ins := userorganization.Create()
	ins.SetUserID(userID)
	ins.SetOrganizationID(organizationID)
	ins.DoNothing()
	if _, err := ins.Insert(ctx, ex); err != nil && !errors.Is(err, runtime.ErrConflict) {
		return fmt.Errorf("could not add the account to the organization: %w", err)
	}
	return nil
}

func (r *userRepository) RemoveOrganization(ctx context.Context, userID, organizationID uuid.UUID) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	if err := userorganization.Delete(ctx, ex, userID, organizationID); err != nil &&
		!errors.Is(err, runtime.ErrNoRow) {
		return fmt.Errorf("could not remove the account from the organization: %w", err)
	}
	return nil
}

func (r *userRepository) AddProject(ctx context.Context, userID, projectID uuid.UUID) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	ins := userproject.Create()
	ins.SetUserID(userID)
	ins.SetProjectID(projectID)
	ins.DoNothing()
	if _, err := ins.Insert(ctx, ex); err != nil && !errors.Is(err, runtime.ErrConflict) {
		return fmt.Errorf("could not add the account to the project: %w", err)
	}
	return nil
}

func (r *userRepository) RemoveProject(ctx context.Context, userID, projectID uuid.UUID) error {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return err
	}
	if err := userproject.Delete(ctx, ex, userID, projectID); err != nil &&
		!errors.Is(err, runtime.ErrNoRow) {
		return fmt.Errorf("could not remove the account from the project: %w", err)
	}
	return nil
}

func (r *userRepository) row(ctx context.Context, preds ...user.Pred) (user.Row, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return user.Row{}, err
	}
	row, found, err := user.New().Where(preds...).One(ctx, ex)
	if err != nil {
		return user.Row{}, fmt.Errorf("could not read the account: %w", err)
	}
	if !found {
		return user.Row{}, fmt.Errorf("%w: no such user", apierr.ErrNotFound)
	}
	return row, nil
}

// hydrate loads an account's memberships.
//
// Two reads rather than a join, for the reason the platform accounts use: a
// join returns one row per membership, which has to be regrouped anyway, and
// the tables are small.
func (r *userRepository) hydrate(ctx context.Context, row user.Row) (models.UserModel, error) {
	ex, err := r.conn.conn.MainExecutor(ctx)
	if err != nil {
		return models.UserModel{}, err
	}
	user, err := accountFrom(row)
	if err != nil {
		return models.UserModel{}, err
	}

	orgs, err := userorganization.New().
		Where(userorganization.UserID.Eq(uuid.UUID(row.ID))).
		Unordered().
		All(ctx, ex, nil)
	if err != nil {
		return models.UserModel{}, fmt.Errorf("could not read the account's organizations: %w", err)
	}
	for _, org := range orgs {
		user.Organizations = append(user.Organizations, models.OrganizationModel{
			Base: models.Base{ID: models.UUID(org.OrganizationID)},
		})
	}

	projects, err := userproject.New().
		Where(userproject.UserID.Eq(uuid.UUID(row.ID))).
		Unordered().
		All(ctx, ex, nil)
	if err != nil {
		return models.UserModel{}, fmt.Errorf("could not read the account's projects: %w", err)
	}
	for _, project := range projects {
		user.Projects = append(user.Projects, models.ProjectModel{
			Base: models.Base{ID: models.UUID(project.ProjectID)},
		})
	}
	return user, nil
}

// accountFrom reads an account's own columns, memberships aside.
func accountFrom(row user.Row) (models.UserModel, error) {
	account := models.UserModel{
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
	}
	if validFrom, ok := row.TokensValidFrom.Get(); ok {
		account.TokensValidFrom = &validFrom
	}
	if issuer, ok := row.IdentityIssuer.Get(); ok {
		account.IdentityIssuer = &issuer
	}
	if subject, ok := row.IdentitySubject.Get(); ok {
		account.IdentitySubject = &subject
	}
	if len(row.Roles) > 0 {
		if err := json.Unmarshal(row.Roles, &account.Roles); err != nil {
			return models.UserModel{}, fmt.Errorf("could not decode an account's roles: %w", err)
		}
	}
	return account, nil
}
