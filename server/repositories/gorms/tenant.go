package gorms

import (
	"context"
	"strings"

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
