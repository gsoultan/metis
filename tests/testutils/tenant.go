package testutils

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/models"
)

// ScopedContext seeds an organization and returns a context that names it,
// the way a request does in production.
//
// Most of this suite entered through a bare `t.Context()`, which carries
// neither a tenant nor a system identity. That works only while the repository
// scope fails open — the behaviour METIS_FEATURE_STRICT_TENANT_SCOPE exists to
// end — so those tests were asserting against a code path no request takes:
// production always arrives through the auth interceptor and the tenant
// resolver, and every query is scoped by what they resolved.
//
// Using this instead is not a way of making the strict-scope run green. It is
// what makes the test resemble the thing it claims to test. A test that passes
// only because the scope failed open is a test that will keep passing when the
// scope stops failing open and the feature breaks.
//
// Deliberately NOT entities.WithSystemContext. That marks work which
// legitimately spans tenants — the job worker, the migration runner — and
// applying it to a test that stands in for a request would silence the warning
// and make the cross-tenant access permanent, which docs/strict-tenant-scope.md
// names as the way this becomes a vulnerability rather than a fix.
func ScopedContext(t *testing.T, repo repositories.Repository) (context.Context, uuid.UUID) {
	t.Helper()
	return ScopedContextFrom(t, repo, t.Context())
}

// ScopedContextFrom is ScopedContext for a caller that already has a context
// it needs to keep — a deadline, or a value set by the test.
func ScopedContextFrom(t *testing.T, repo repositories.Repository, ctx context.Context) (context.Context, uuid.UUID) {
	t.Helper()

	orgID := uuid.Must(uuid.NewV7())
	// Seeded as system work, because creating the organization is the moment
	// before there is a tenant to be scoped by. Everything after it is scoped.
	seedCtx := entities.WithSystemContext(ctx)
	if err := repo.Organization().Create(seedCtx, models.OrganizationModel{
		Base: models.Base{ID: models.UUID(orgID)},
		Name: "Test Org",
	}); err != nil {
		t.Fatalf("seed organization: %v", err)
	}

	return entities.WithTenantContext(ctx, entities.TenantContext{TenantID: orgID.String()}), orgID
}

// OrgIDFrom reads back the organization a ScopedContext names.
//
// Seeding a row that belongs to an organization needs the id, and passing it
// alongside the context everywhere doubles every helper signature. The context
// already carries it, so read it from there.
func OrgIDFrom(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	tc, ok := entities.TenantContextFrom(ctx)
	if !ok {
		t.Fatal("context carries no tenant; build it with ScopedContext")
	}
	id, err := uuid.Parse(tc.TenantID)
	if err != nil {
		t.Fatalf("tenant id %q is not a uuid: %v", tc.TenantID, err)
	}
	return id
}

// ScopedProject is ScopedContext plus a project inside that organization.
//
// Most rows in this product hang off a project — definitions, decisions,
// instances, tasks, forms — and the tenant scope reaches them by joining
// through it. A row seeded with no project is unreachable by any scoped read,
// which is true in production too: it belongs to nobody.
func ScopedProject(t *testing.T, repo repositories.Repository) (context.Context, uuid.UUID, uuid.UUID) {
	t.Helper()

	ctx, orgID := ScopedContext(t, repo)
	projectID := uuid.Must(uuid.NewV7())
	if err := repo.Project().Create(entities.WithSystemContext(ctx), models.ProjectModel{
		Base:           models.Base{ID: models.UUID(projectID)},
		OrganizationID: models.UUID(orgID),
		Name:           "Test Project",
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return ctx, orgID, projectID
}
