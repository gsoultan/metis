package pg

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/internal/pkg/tenantscope"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/processinstance"
	"github.com/gsoultan/metis/server/repositories/store/project"
)

// Tenant scoping, expressed as a predicate rather than a join.
//
// The GORM layer scoped by joining `projects` and comparing its
// organization_id. storm has no subquery predicate and its semi-join probes go
// the other way — parent-has-children, not child-belongs-to-parent — so the
// equivalent here resolves which projects the caller may see and filters on
// project_id.
//
// That is one extra read, and it buys two things beyond parity. A join fans out
// and needs DISTINCT once anything else joins; a predicate does not. And the
// project list is a value, so a caller can be shown *which* projects a refusal
// covered, where a join can only return no rows.
//
// The failure mode is the one that matters: a context carrying neither a tenant
// nor the system marker resolves to no projects at all, so every scoped read
// returns nothing and every scoped write is refused. Same as the GORM layer,
// deliberately — see entities.WithSystemContext for why "background work" and
// "somebody forgot to resolve the tenant" must not be the same thing.

// tenantScope is who a request may see.
type tenantScope struct {
	// organization is the tenant, or uuid.Nil for system work.
	organization uuid.UUID
	// projects are the ids in that organization. Empty and system is
	// everything; empty and not system is nothing.
	projects []uuid.UUID
	// system marks work that legitimately spans every tenant — the job worker,
	// the timer sweep, the migration runner.
	system bool
}

// unrestricted reports whether this scope filters at all.
func (s tenantScope) unrestricted() bool { return s.system }

// scopeOf resolves the caller's scope, reading the project list when there is a
// tenant to read it for.
//
// System work skips the read entirely: it spans every tenant, so there is no
// list to fetch and fetching one would be a query per background tick.
func (r *conn) scopeOf(ctx context.Context) (tenantScope, error) {
	tc, ok := entities.TenantContextFrom(ctx)
	if !ok || tc.TenantID == "" {
		// Neither a tenant nor a resolvable one. tenantscope.Allowed decides
		// what that means: system work spans every tenant, and anything else
		// sees nothing once the strict scope is on. The flag is what keeps
		// this from changing behaviour on an installation that upgrades into
		// it — see internal/pkg/tenantscope.
		return tenantScope{system: tenantscope.Allowed(ctx)}, nil
	}
	organization, err := uuid.Parse(tc.TenantID)
	if err != nil {
		return tenantScope{}, apierr.Invalidf("tenant %q is not a valid identifier", tc.TenantID)
	}

	projects, err := r.projectsOf(ctx, organization)
	if err != nil {
		return tenantScope{}, err
	}
	return tenantScope{organization: organization, projects: projects}, nil
}

// projectsOf lists an organization's projects.
//
// Read every time rather than cached. A cache here would have to be invalidated
// by the project repository, and the window where it is not is the window where
// somebody creates a project, deploys into it and finds it empty — which is
// indistinguishable from the feature being broken. The read is one indexed
// lookup returning a handful of ids.
//
// Every project, not the store's first thousand. This list is the scope: a
// project missing from it is one whose rows the organization cannot read and
// whose writes are refused, so an organization with more than a thousand lost
// the rest. Walked in key order, which the keyset cursor needs; for a list of a
// handful that is one statement, as before.
func (r *conn) projectsOf(ctx context.Context, organization uuid.UUID) ([]uuid.UUID, error) {
	ex, err := r.conn.MainExecutor(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := everyRow[project.Row](ctx, ex, project.New().
		Where(project.OrganizationID.Eq(organization)))
	if err != nil {
		return nil, fmt.Errorf("could not read the projects in this organization: %w", err)
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

// requireProjectInTenant refuses a project that is not the caller's.
//
// This is the check that makes a create safe. A create names its parent
// project, and without this a caller could point one at another organization's
// project — the row would then be scoped to that tenant, so the attacker could
// not read back what they had planted, but it would be there.
func (r *conn) requireProjectInTenant(ctx context.Context, projectID uuid.UUID) error {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return err
	}
	if scope.unrestricted() || projectID == uuid.Nil {
		return nil
	}
	for _, id := range scope.projects {
		if id == projectID {
			return nil
		}
	}
	// Not found rather than forbidden: from this caller's point of view there
	// is no such project, which is also the honest answer when there is one and
	// it belongs to somebody else.
	return fmt.Errorf("%w: no such project", apierr.ErrNotFound)
}

// requireOwnOrganization refuses an organization that is not the caller's.
func (r *conn) requireOwnOrganization(ctx context.Context, organizationID uuid.UUID) error {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return err
	}
	if scope.unrestricted() {
		return nil
	}
	if scope.organization != uuid.Nil && scope.organization == organizationID {
		return nil
	}
	return fmt.Errorf("%w: no such organization", apierr.ErrNotFound)
}

// conn is embedded by every repository in this package: the connection, plus
// the scoping every one of them has to apply.
type conn struct {
	conn *db.Conn
}

var _ = db.ErrEnvironmentUnavailable

// requireInstanceInTenant refuses an instance that is not the caller's.
//
// Several tables hang off an instance and carry no project of their own — a
// snapshot, a compensation record, an audit entry read by instance. Scoping
// them means asking whether the instance is visible, which is one indexed read
// and the same answer every one of them needs.
func (r *conn) requireInstanceInTenant(ctx context.Context, instanceID uuid.UUID) error {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return err
	}
	if scope.unrestricted() || instanceID == uuid.Nil {
		return nil
	}
	if len(scope.projects) == 0 {
		return fmt.Errorf("%w: no such process instance", apierr.ErrNotFound)
	}

	// The environment's database, not main: an instance belongs to the runtime
	// it is running in. Only the identity and registry tables are
	// installation-wide.
	ex, err := r.conn.Executor(ctx)
	if err != nil {
		return err
	}
	found, err := processinstance.New().
		Where(
			processinstance.ID.Eq(instanceID),
			processinstance.ProjectID.In(uuidsToRaw(scope.projects)...),
		).
		Exists(ctx, ex)
	if err != nil {
		return fmt.Errorf("could not check the process instance: %w", err)
	}
	if !found {
		return fmt.Errorf("%w: no such process instance", apierr.ErrNotFound)
	}
	return nil
}

// scopedProjects narrows a requested project to what the caller may see.
//
// visible is false when the caller may see nothing, which a list answers with
// no rows and a read answers with not-found. When it is true, projects is the
// filter to apply — and nil there means "do not filter at all", which only
// system work gets and only when it named no project.
//
// The two are separate returns because a single nil slice meant both "nothing
// is visible" and "everything is", and those are opposite answers.
func (r *conn) scopedProjects(ctx context.Context, requested uuid.UUID) (projects []uuid.UUID, visible bool, err error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return nil, false, err
	}
	if scope.unrestricted() {
		if requested != uuid.Nil {
			return []uuid.UUID{requested}, true, nil
		}
		return nil, true, nil
	}
	if len(scope.projects) == 0 {
		return nil, false, nil
	}
	if requested == uuid.Nil {
		return scope.projects, true, nil
	}
	for _, id := range scope.projects {
		if id == requested {
			return []uuid.UUID{requested}, true, nil
		}
	}
	return nil, false, nil
}

// canSeeInstance answers requireInstanceInTenant as a boolean.
//
// For the lists that turn "not yours" into no rows rather than an error. A
// function whose error the caller must discard reads like a mistake, and is one
// often enough that the linter refuses it.
func (r *conn) canSeeInstance(ctx context.Context, instanceID uuid.UUID) bool {
	return r.requireInstanceInTenant(ctx, instanceID) == nil
}
