package connector_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/app"
	"github.com/gsoultan/metis/internal/pkg/health"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// Connectors are installation-wide: a step in any organization that names a key
// runs whatever is installed under it, with that organization's own connection
// attached. Roles are global too, so the administrator of any one organization
// passed the gate on installing one — and could put an address of their own
// under a key every other organization's steps call with their credentials.
//
// These go through the real HTTP chain, because the gate is the chain: the
// service installs whatever it is handed.

const (
	// sharedConnector is already installed when each test starts; the switch
	// and the removal act on it.
	sharedConnector = "key: crm.shared\nversion: 1\nrequest:\n  url: https://crm.example.com/leads\n"

	anotherConnector = "key: crm.another\nversion: 1\nrequest:\n  url: https://crm.example.com/contacts\n"

	sharedSpecification = `
openapi: 3.0.3
info: {title: Shared CRM, version: "1"}
servers: [{url: "https://crm.example.com"}]
paths:
  /accounts:
    post: {operationId: createAccount, responses: {"201": {description: ok}}}
`
)

// platformHarness is an installation with the organizations a test names,
// served over HTTP. It borrows executeHarness's sign-in and POST.
type platformHarness struct {
	executeHarness
	svc  services.ServiceFacade
	orgs map[string]uuid.UUID
}

func newPlatformHarness(t *testing.T, organizations ...string) *platformHarness {
	t.Helper()
	db := testutils.SetupTestDB(t)
	conn := testutils.StormConn(db)
	repo := repositories.NewRepository(conn)
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "platform-test-secret", nil, nil, nil, func(*gorm.DB) {})
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, conn)
	h := &platformHarness{
		executeHarness: executeHarness{server: httptest.NewServer(handler), tokens: map[string]string{}},
		svc:            svc,
		orgs:           map[string]uuid.UUID{},
	}
	t.Cleanup(h.server.Close)

	for _, name := range organizations {
		org, err := svc.CreateOrganization(context.Background(), name, "")
		if err != nil {
			t.Fatalf("create organization %s: %v", name, err)
		}
		h.orgs[name] = org.ID
	}
	return h
}

// administrator creates an administrator of one organization under the
// account id given, and signs them in.
func (h *platformHarness) administrator(t *testing.T, id uuid.UUID, username, organization string) string {
	t.Helper()
	org := h.orgs[organization]
	tctx := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: org.String()})
	if err := h.svc.CreateUser(tctx, entities.User{
		ID:            id,
		Username:      username,
		Roles:         []string{entities.RoleAdmin},
		Organizations: []*entities.Organization{{ID: org}},
	}, "execute-test-password"); err != nil {
		t.Fatalf("create %s: %v", username, err)
	}
	return h.login(t, username)
}

// installShared installs sharedConnector the way the installation already had
// it, and returns its id.
func (h *platformHarness) installShared(t *testing.T) uuid.UUID {
	t.Helper()
	installed, err := h.svc.InstallManifest(entities.WithSystemContext(t.Context()), []byte(sharedConnector))
	if err != nil {
		t.Fatalf("install the shared connector: %v", err)
	}
	return installed.ID
}

func (h *platformHarness) remove(t *testing.T, token, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, h.server.URL+path, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(raw)
}

// manifestChange is one way of changing the connectors every organization runs.
type manifestChange struct {
	name string
	as   func(token string) (int, string)
}

// manifestChanges are installing, importing, switching off and removing, in an
// order each can follow the one before. target is an installed connector.
func (h *platformHarness) manifestChanges(t *testing.T, target uuid.UUID) []manifestChange {
	return []manifestChange{
		{"install a connector", func(token string) (int, string) {
			return h.post(t, token, "/api/v1/connector-manifests", map[string]any{"document": anotherConnector, "format": "manifest"})
		}},
		{"import an OpenAPI document", func(token string) (int, string) {
			return h.post(t, token, "/api/v1/connector-manifests", map[string]any{"document": sharedSpecification, "format": "openapi"})
		}},
		{"switch a connector off", func(token string) (int, string) {
			return h.post(t, token, "/api/v1/connector-manifests/"+target.String()+"/enabled", map[string]any{"enabled": false})
		}},
		{"remove a connector", func(token string) (int, string) {
			return h.remove(t, token, "/api/v1/connector-manifests/"+target.String())
		}},
	}
}

func TestOnAnInstallationOfSeveralOrganizationsOnlyAPlatformAdministratorChangesItsConnectors(t *testing.T) {
	platformAdministrator := uuid.Must(uuid.NewV7())
	t.Setenv("METIS_PLATFORM_ADMINS", platformAdministrator.String())
	h := newPlatformHarness(t, "Acme", "Globex")
	organizationAdministrator := h.administrator(t, uuid.Must(uuid.NewV7()), "globex-admin", "Globex")
	operatorsChoice := h.administrator(t, platformAdministrator, "platform-admin", "Acme")
	shared := h.installShared(t)
	changes := h.manifestChanges(t, shared)

	for _, change := range changes {
		status, body := change.as(organizationAdministrator)
		if status != http.StatusForbidden {
			t.Errorf("an administrator of one organization could %s every organization runs: status %d (%s)",
				change.name, status, body)
			continue
		}
		if !strings.Contains(body, "METIS_PLATFORM_ADMINS") {
			t.Errorf("refused to %s without saying who can grant it: %s", change.name, body)
		}
	}

	catalogue, err := h.svc.ListManifests(entities.WithSystemContext(t.Context()))
	if err != nil {
		t.Fatalf("list the installed connectors: %v", err)
	}
	if len(catalogue) != 1 || catalogue[0].Key != "crm.shared" || !catalogue[0].Enabled {
		t.Fatalf("after the refusals the installation runs %+v, want only crm.shared, switched on", catalogue)
	}

	// The administrator the operator named can do every one of them.
	for _, change := range changes {
		if status, body := change.as(operatorsChoice); status != http.StatusOK {
			t.Errorf("the platform administrator could not %s: status %d (%s)", change.name, status, body)
		}
	}
}

// An installation with one organization is administered by that
// organization's administrators, and has nobody else to ask: it keeps working
// with nothing configured.
func TestOnAnInstallationOfOneOrganizationItsAdministratorsStillChangeItsConnectors(t *testing.T) {
	t.Setenv("METIS_PLATFORM_ADMINS", "")
	h := newPlatformHarness(t, "Acme")
	administrator := h.administrator(t, uuid.Must(uuid.NewV7()), "acme-admin", "Acme")
	shared := h.installShared(t)

	for _, change := range h.manifestChanges(t, shared) {
		if status, body := change.as(administrator); status != http.StatusOK {
			t.Errorf("the administrator of the only organization could not %s: status %d (%s)", change.name, status, body)
		}
	}
}
