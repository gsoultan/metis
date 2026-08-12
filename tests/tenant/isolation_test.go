package tenant_test

import (
	"context"
	"errors"
	"testing"

	"github.com/go-kit/kit/endpoint"
	"github.com/google/uuid"
	pkgauth "github.com/gsoultan/gobpm/internal/pkg/auth"
	"github.com/gsoultan/gobpm/server/domains/adapters"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/interceptors/tenant"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/server/repositories/models"
	"github.com/gsoultan/gobpm/tests/testutils"
)

// Tenant isolation was written and then wired to nothing: the resolver was
// never in the request path, so TenantContext was never populated and
// tenantScopeDB returned the unscoped database on every call. Every
// authenticated user could read every organization's data.
//
// These tests assert the two halves separately — that the resolver derives the
// tenant from the authenticated principal rather than a client header, and that
// repositories actually filter once it does.

func ctxWithUser(orgIDs ...uuid.UUID) context.Context {
	orgs := make([]*entities.Organization, 0, len(orgIDs))
	for _, id := range orgIDs {
		orgs = append(orgs, &entities.Organization{ID: id})
	}
	return context.WithValue(context.Background(), pkgauth.UserContextKey, entities.User{
		Username:      "tester",
		Organizations: orgs,
	})
}

// capture returns an endpoint that records the TenantContext it was called with.
func capture(got *entities.TenantContext) endpoint.Endpoint {
	return func(ctx context.Context, _ any) (any, error) {
		tc, _ := entities.TenantContextFrom(ctx)
		*got = tc
		return nil, nil
	}
}

func TestTenantResolver_DerivesTenantFromAuthenticatedPrincipal(t *testing.T) {
	org := uuid.Must(uuid.NewV7())
	var got entities.TenantContext

	guarded := tenant.NewEndpointTenantResolver().Intercept(capture(&got))
	if _, err := guarded(ctxWithUser(org), nil); err != nil {
		t.Fatalf("resolver rejected a legitimate single-org caller: %v", err)
	}

	if got.TenantID != org.String() {
		t.Fatalf("TenantID = %q, want %q", got.TenantID, org.String())
	}
}

// The core of the old vulnerability: the tenant came straight from a
// client-controlled header, so changing one header read another org's data.
func TestTenantResolver_RejectsOrganizationTheCallerDoesNotBelongTo(t *testing.T) {
	mine := uuid.Must(uuid.NewV7())
	theirs := uuid.Must(uuid.NewV7())
	var got entities.TenantContext

	guarded := tenant.NewEndpointTenantResolver().Intercept(capture(&got))
	ctx := tenant.WithRequestedOrganization(ctxWithUser(mine), theirs.String())

	_, err := guarded(ctx, nil)
	if !errors.Is(err, pkgauth.ErrUnauthorized) {
		t.Fatalf("caller reached a foreign organization by asserting a header: got %v, want ErrUnauthorized", err)
	}
	if got.TenantID != "" {
		t.Fatalf("endpoint executed with tenant %q despite the membership check failing", got.TenantID)
	}
}

// A member of several organizations may choose between them.
func TestTenantResolver_AllowsSelectingAmongOwnMemberships(t *testing.T) {
	first := uuid.Must(uuid.NewV7())
	second := uuid.Must(uuid.NewV7())
	var got entities.TenantContext

	guarded := tenant.NewEndpointTenantResolver().Intercept(capture(&got))
	ctx := tenant.WithRequestedOrganization(ctxWithUser(first, second), second.String())

	if _, err := guarded(ctx, nil); err != nil {
		t.Fatalf("multi-org caller could not select their own second org: %v", err)
	}
	if got.TenantID != second.String() {
		t.Fatalf("TenantID = %q, want %q", got.TenantID, second.String())
	}
}

func TestTenantResolver_RejectsPrincipalWithNoMembership(t *testing.T) {
	guarded := tenant.NewEndpointTenantResolver().Intercept(capture(new(entities.TenantContext)))

	if _, err := guarded(ctxWithUser(), nil); !errors.Is(err, tenant.ErrNoTenantMembership) {
		t.Fatalf("principal with no organization: got %v, want ErrNoTenantMembership", err)
	}
}

// The other half: with a TenantContext present, repositories must filter.
func TestRepositories_ScopeListsToTheActiveTenant(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	ctx := t.Context()

	orgA, projA := seedOrgWithProject(t, repo, "Org A")
	orgB, projB := seedOrgWithProject(t, repo, "Org B")

	seedDefinition(t, repo, projA, "def-a")
	seedDefinition(t, repo, projB, "def-b")
	seedTask(t, repo, projA, "task-a")
	seedTask(t, repo, projB, "task-b")

	t.Run("definitions", func(t *testing.T) {
		got, err := repo.Definition().List(entities.WithTenantContext(ctx, entities.TenantContext{TenantID: orgA.String()}))
		if err != nil {
			t.Fatalf("list definitions: %v", err)
		}
		assertOnlyKeys(t, "definitions", keysOf(got), "def-a")
	})

	t.Run("tasks", func(t *testing.T) {
		got, err := repo.Task().List(entities.WithTenantContext(ctx, entities.TenantContext{TenantID: orgB.String()}))
		if err != nil {
			t.Fatalf("list tasks: %v", err)
		}
		names := make([]string, 0, len(got))
		for _, m := range got {
			names = append(names, m.Name)
		}
		assertOnlyKeys(t, "tasks", names, "task-b")
	})

	// Without a TenantContext the repository is unscoped. That is the
	// documented behaviour for internal/system calls such as the job worker,
	// which runs outside any request. It is only safe because the endpoint
	// chain always populates the context for external callers — which is what
	// the resolver tests above guarantee.
	t.Run("system calls see everything", func(t *testing.T) {
		got, err := repo.Definition().List(ctx)
		if err != nil {
			t.Fatalf("list definitions: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("unscoped list returned %d definitions, want 2", len(got))
		}
	})
}

func keysOf(ms []models.ProcessDefinitionModel) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.Key)
	}
	return out
}

func assertOnlyKeys(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %v, want exactly %v — cross-tenant rows leaked", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: got %v, want %v", what, got, want)
		}
	}
}

func seedOrgWithProject(t *testing.T, repo repositories.Repository, name string) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := t.Context()

	orgID := uuid.Must(uuid.NewV7())
	if err := repo.Organization().Create(ctx, models.OrganizationModel{
		Base: models.Base{ID: orgID},
		Name: name,
	}); err != nil {
		t.Fatalf("seed org %s: %v", name, err)
	}

	projID := uuid.Must(uuid.NewV7())
	if err := repo.Project().Create(ctx, models.ProjectModel{
		Base:           models.Base{ID: projID},
		OrganizationID: orgID,
		Name:           name + " Project",
	}); err != nil {
		t.Fatalf("seed project for %s: %v", name, err)
	}
	return orgID, projID
}

func seedDefinition(t *testing.T, repo repositories.Repository, projectID uuid.UUID, key string) {
	t.Helper()
	def := entities.ProcessDefinition{
		ID:      uuid.Must(uuid.NewV7()),
		Project: &entities.Project{ID: projectID},
		Key:     key,
		Name:    key,
		Version: 1,
		Nodes:   []*entities.Node{{ID: "start", Type: entities.StartEvent}},
	}
	if err := repo.Definition().Create(t.Context(), adapters.DefinitionModelAdapter{Definition: &def}.ToModel()); err != nil {
		t.Fatalf("seed definition %s: %v", key, err)
	}
}

func seedTask(t *testing.T, repo repositories.Repository, projectID uuid.UUID, name string) {
	t.Helper()
	task := entities.Task{
		ID:      uuid.Must(uuid.NewV7()),
		Project: &entities.Project{ID: projectID},
		Name:    name,
		Status:  entities.TaskUnclaimed,
		Node:    &entities.Node{ID: "n1"},
	}
	if err := repo.Task().Create(t.Context(), adapters.TaskModelAdapter{Task: task}.ToModel()); err != nil {
		t.Fatalf("seed task %s: %v", name, err)
	}
}
