package gorms

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/features"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/rs/zerolog/log"
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
	if !features.Enabled(features.StrictTenantScope) {
		return true
	}
	reportUnidentifiedAccess()
	return false
}

// reportedSites remembers which call sites have already been named.
//
// Keyed by program counter, so it is bounded by the amount of code that can
// reach here rather than by traffic — this is not a map keyed on anything a
// caller supplies.
var reportedSites sync.Map

// reportUnidentifiedAccess names the code path that reached a repository with
// neither a tenant nor a system identity.
//
// **The strict scope's failure mode is silence.** A background entry point that
// forgets to mark itself does not error — it reads nothing, and an engine that
// reads nothing looks like an engine with no work to do. That is precisely what
// makes turning this flag on hard to evaluate: an operator watching a staging
// environment has to notice an *absence*, and absences are what people miss.
//
// So each distinct site says so. Once, not every time: these sit on poll loops
// that run every couple of seconds, and the useful output is the list of paths
// that still need an identity — not a count of how often they ran. Turning a
// rollout from "watch for something that stops happening" into "read this list"
// is the whole point.
//
// Nothing is logged while the flag is off, because this is unreachable then.
func reportUnidentifiedAccess() {
	site, from, ok := deniedCallSite()
	if !ok {
		return
	}
	if _, seen := reportedSites.LoadOrStore(site.PC, struct{}{}); seen {
		return
	}
	event := log.Warn().
		Str("repository", site.Function).
		Str("at", fmt.Sprintf("%s:%d", site.File, site.Line)).
		Str("flag", features.EnvName(features.StrictTenantScope))
	if from != "" {
		// The repository method says *what* was denied; its caller says which
		// path forgot an identity, which is the thing that has to change.
		event = event.Str("called_from", from)
	}
	event.Msg("A repository query carried neither a tenant nor a system identity, so it was answered with nothing. " +
		"This path needs entities.WithSystemContext if it is background work, or a resolved tenant if it serves a request.")
}

// denyAll returns a query guaranteed to match nothing.
//
// An empty result rather than an error, because a repository's contract is rows
// and every caller already handles finding none. What makes that safe to do
// quietly is reportUnidentifiedAccess above, which names the path that got here
// without an identity.
func denyAll(db *gorm.DB) *gorm.DB {
	return db.Where(QueryDenyAll)
}

// scopeHelperFile is where the tenant-scope helpers live. Frames from it are
// skipped when naming a denial: they are the same three functions every time
// and identify nothing.
const scopeHelperFile = "/gorms/tenant.go"

// deniedCallSite returns the repository method that was denied and, when it can
// be told, the caller outside this package that invoked it.
//
// Both, because they answer different questions. The repository method says
// what came back empty; its caller is the path that failed to carry an identity
// and therefore the code that has to change.
func deniedCallSite() (site runtime.Frame, calledFrom string, ok bool) {
	pc := make([]uintptr, 24)
	// Skip runtime.Callers, this function and reportUnidentifiedAccess.
	n := runtime.Callers(3, pc)
	if n == 0 {
		return runtime.Frame{}, "", false
	}

	frames := runtime.CallersFrames(pc[:n])
	const gormsPackage = "/server/repositories/gorms."
	for {
		frame, more := frames.Next()
		switch {
		case strings.HasSuffix(frame.File, scopeHelperFile):
			// A scope helper — keep looking for the repository method.
		case site.PC == 0:
			site = frame
		case !strings.Contains(frame.Function, gormsPackage):
			return site, frame.Function, true
		}
		if !more {
			return site, calledFrom, site.PC != 0
		}
	}
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
