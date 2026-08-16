package gorms

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/internal/pkg/features"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"gorm.io/gorm"
)

// QueryDenyAll matches nothing. It is how a query with no tenant identity is
// answered once strict scoping is on: an empty result rather than an error,
// because a repository's contract is rows, and every caller already handles
// finding none.
const QueryDenyAll = "1 = 0"

// unscopedAccessAllowed decides what a context carrying no tenant may see.
//
// System work — the engine, the job worker, message consumers, migrations —
// legitimately spans every tenant and says so explicitly. Anything else is a
// path that failed to resolve a tenant, which AGENTS §2.3 says must deny.
//
// Behind a flag because the failure mode of getting it wrong is quiet: a
// background entry point that forgets to mark itself does not error, it reads
// nothing, and an engine that reads nothing looks like an engine with no work.
// Off by default, so upgrading changes nothing until someone decides otherwise.
func unscopedAccessAllowed(ctx context.Context) bool {
	if entities.IsSystemContext(ctx) {
		return true
	}
	return !features.Enabled(features.StrictTenantScope)
}

// denyAll returns a query guaranteed to match nothing.
func denyAll(db *gorm.DB) *gorm.DB {
	return db.Where(QueryDenyAll)
}

// tenantScopeDB returns a *gorm.DB scoped to the active tenant (organization)
// extracted from the request context via TenantContext. It joins through the
// projects table so list queries only return records belonging to the caller's
// organization.
//
// If no TenantContext is present (e.g. internal/system calls), the original db
// is returned unchanged so the caller can still function without tenant context.
//
// table must be the SQL table name of the model being queried (e.g. "tasks",
// "process_instances") so the JOIN clause can be built correctly.
func tenantScopeDB(ctx context.Context, db *gorm.DB, table string) *gorm.DB {
	tc, ok := entities.TenantContextFrom(ctx)
	if !ok || tc.TenantID == "" {
		if unscopedAccessAllowed(ctx) {
			return db
		}
		return denyAll(db)
	}

	joinClause := strings.ReplaceAll(QueryTenantScopeViaProject, "{table}", table)
	return db.Joins(joinClause, tc.TenantID)
}

// tenantScopeDBOptionalProject is tenantScopeDB for tables whose project_id is
// nullable. Rows that carry a project are scoped to the caller's organization;
// rows that carry none are left visible, because they are reachable only
// through a column that already names their owner (a notification's user_id).
//
// An inner join would have deleted those rows from every result set instead.
func tenantScopeDBOptionalProject(ctx context.Context, db *gorm.DB, table string) *gorm.DB {
	tc, ok := entities.TenantContextFrom(ctx)
	if !ok || tc.TenantID == "" {
		if unscopedAccessAllowed(ctx) {
			return db
		}
		return denyAll(db)
	}

	joinClause := strings.ReplaceAll(QueryTenantScopeViaProjectOptional, "{table}", table)
	condition := strings.ReplaceAll(QueryTenantScopeOptionalCondition, "{table}", table)
	return db.Joins(joinClause).Where(condition, tc.TenantID)
}

// tenantScopeCondition is tenantScopeDB expressed as a WHERE predicate instead
// of a JOIN, for locking reads and for statements where join syntax is not
// portable. It scopes the same rows; it just does not widen the statement's
// lock footprint to the projects table.
func tenantScopeCondition(ctx context.Context, db *gorm.DB, table string) *gorm.DB {
	tc, ok := entities.TenantContextFrom(ctx)
	if !ok || tc.TenantID == "" {
		if unscopedAccessAllowed(ctx) {
			return db
		}
		return denyAll(db)
	}

	condition := strings.ReplaceAll(QueryTenantScopeViaProjectSubquery, "{table}", table)
	return db.Where(condition, tc.TenantID)
}

// tenantScopeOrganization scopes a table that carries organization_id directly,
// which today means projects alone.
//
// Projects were the one table nothing scoped, because they are what every other
// scope joins through — so List returned every organization's projects, and a
// caller could name any project as the parent of something they created.
func tenantScopeOrganization(ctx context.Context, db *gorm.DB, table string) *gorm.DB {
	tc, ok := entities.TenantContextFrom(ctx)
	if !ok || tc.TenantID == "" {
		if unscopedAccessAllowed(ctx) {
			return db
		}
		return denyAll(db)
	}

	condition := strings.ReplaceAll(QueryTenantScopeDirect, "{table}", table)
	return db.Where(condition, tc.TenantID)
}

// requireOwnOrganization refuses a write that names an organization other than
// the caller's. It is the create-side counterpart of the read scope: the scope
// stops a caller reading another tenant's rows, this stops them writing rows
// into it.
func requireOwnOrganization(ctx context.Context, organizationID uuid.UUID) error {
	tc, ok := entities.TenantContextFrom(ctx)
	if !ok || tc.TenantID == "" {
		if unscopedAccessAllowed(ctx) {
			return nil
		}
		return gorm.ErrRecordNotFound
	}
	if organizationID == uuid.Nil {
		return nil
	}
	if organizationID.String() != tc.TenantID {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// requireProjectInTenant returns ErrRecordNotFound unless the project belongs to
// the caller's organization.
//
// This is the check that makes Create safe. A create request names its parent
// project, and without this a caller could point one at another organization's
// project — the row would then be scoped to *that* tenant, so the attacker could
// not even read back what they had planted, but it would be there.
func requireProjectInTenant(ctx context.Context, db *gorm.DB, projectID uuid.UUID) error {
	tc, ok := entities.TenantContextFrom(ctx)
	if !ok || tc.TenantID == "" {
		if unscopedAccessAllowed(ctx) {
			return nil
		}
		return gorm.ErrRecordNotFound
	}
	if projectID == uuid.Nil {
		return nil
	}

	var count int64
	if err := tenantScopeOrganization(ctx, db.Model(&models.ProjectModel{}), tableProjects).
		Where(QualifiedByID(tableProjects), projectID).
		Count(&count).Error; err != nil {
		return fmt.Errorf("could not check project ownership: %w", err)
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// tenantScopeConditionOptionalProject is tenantScopeCondition for tables whose
// project_id is nullable.
func tenantScopeConditionOptionalProject(ctx context.Context, db *gorm.DB, table string) *gorm.DB {
	tc, ok := entities.TenantContextFrom(ctx)
	if !ok || tc.TenantID == "" {
		if unscopedAccessAllowed(ctx) {
			return db
		}
		return denyAll(db)
	}

	condition := strings.ReplaceAll(QueryTenantScopeViaProjectSubqueryOptional, "{table}", table)
	return db.Where(condition, tc.TenantID)
}

// requireVisibleToTenant returns ErrRecordNotFound unless the row is inside the
// caller's tenant scope, and nil when there is no tenant to scope by — engine
// and background work is unguarded here for the same reason the read scope lets
// it through.
//
// Writes are guarded with a scoped read rather than a scoped UPDATE or DELETE
// because neither alternative is safe:
//
//   - GORM's Save ignores a preceding Where. Verified on SQLite, PostgreSQL and
//     MySQL: the row updates anyway. A scope written that way would read as
//     applied and enforce nothing, which is worse than none at all.
//   - Rewriting Save as Model().Where().Updates() changes which columns are
//     written, because Updates skips zero values. Clearing a field would quietly
//     stop persisting.
//
// The cost is a check-then-write window. These rows do not change project in
// normal operation, and when a unit of work is active both statements run inside
// its transaction.
func requireVisibleToTenant(ctx context.Context, db *gorm.DB, table string, model any, id uuid.UUID) error {
	return requireVisible(ctx, tenantScopeCondition, db, table, model, id)
}

// requireVisibleToTenantOptionalProject is requireVisibleToTenant for tables
// whose project_id is nullable.
func requireVisibleToTenantOptionalProject(ctx context.Context, db *gorm.DB, table string, model any, id uuid.UUID) error {
	return requireVisible(ctx, tenantScopeConditionOptionalProject, db, table, model, id)
}

func requireVisible(
	ctx context.Context,
	scope func(context.Context, *gorm.DB, string) *gorm.DB,
	db *gorm.DB,
	table string,
	model any,
	id uuid.UUID,
) error {
	tc, ok := entities.TenantContextFrom(ctx)
	if !ok || tc.TenantID == "" {
		if unscopedAccessAllowed(ctx) {
			return nil
		}
		return gorm.ErrRecordNotFound
	}

	var count int64
	if err := scope(ctx, db.Model(model), table).
		Where(QualifiedByID(table), id).
		Count(&count).Error; err != nil {
		return fmt.Errorf("could not check tenant ownership: %w", err)
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// tenantScopeDeploymentResources scopes deployment_resources through their
// parent deployment, which is where the project — and therefore the tenant —
// actually lives.
func tenantScopeDeploymentResources(ctx context.Context, db *gorm.DB) *gorm.DB {
	tc, ok := entities.TenantContextFrom(ctx)
	if !ok || tc.TenantID == "" {
		if unscopedAccessAllowed(ctx) {
			return db
		}
		return denyAll(db)
	}

	return db.Joins(QueryTenantScopeViaDeployment, tc.TenantID)
}
