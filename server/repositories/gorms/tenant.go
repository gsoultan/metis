package gorms

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"gorm.io/gorm"
)

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
		return db
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
		return db
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
		return db
	}

	condition := strings.ReplaceAll(QueryTenantScopeViaProjectSubquery, "{table}", table)
	return db.Where(condition, tc.TenantID)
}

// tenantScopeConditionOptionalProject is tenantScopeCondition for tables whose
// project_id is nullable.
func tenantScopeConditionOptionalProject(ctx context.Context, db *gorm.DB, table string) *gorm.DB {
	tc, ok := entities.TenantContextFrom(ctx)
	if !ok || tc.TenantID == "" {
		return db
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
		return nil
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
		return db
	}

	return db.Joins(QueryTenantScopeViaDeployment, tc.TenantID)
}
