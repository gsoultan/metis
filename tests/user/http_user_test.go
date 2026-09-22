package user_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gsoultan/metis/server/domains/entities"
	handlersimpl "github.com/gsoultan/metis/server/domains/handlers/impl"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	service_impl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/transports/https"
	"github.com/gsoultan/metis/tests/testutils"
)

func setupHTTPTestService(t *testing.T) (services.ServiceFacade, http.Handler) {
	t.Helper()
	// The shared harness rather than a hand-rolled database and model list.
	// Its own list drifted from the real one — a model added to the schema was
	// simply absent here — and it opened SQLite, which the product no longer
	// runs on and which has no storm connection for the ported repositories.
	db := testutils.SetupTestDB(t)

	repo := repositories.NewRepository(testutils.StormConn(db))
	dispatcher := impl.NewEventDispatcher()

	orgSvc := service_impl.NewOrganizationService(repo)
	projectSvc := service_impl.NewProjectService(repo)
	defSvc := service_impl.NewDefinitionService(repo)
	connectorSvc := service_impl.NewConnectorService(repo)
	engine := service_impl.NewExecutionEngine(repo, dispatcher)
	taskSvc := service_impl.NewTaskService(repo, engine, service_impl.NewAuditWriter(repo.Audit()))
	jobSvc := service_impl.NewJobService(repo, engine, connectorSvc, service_impl.NewNoOpLocker(), handlersimpl.NewErrorBoundaryMatcher())
	externalTaskSvc := service_impl.NewExternalTaskService(repo, engine)
	decisionSvc := service_impl.NewDecisionService(repo, service_impl.NewDecisionTableEvaluator(service_impl.NewFEELEvaluator()))
	migrationSvc := service_impl.NewMigrationService(repo, engine)
	sse := impl.NewSSEObserver()
	collaborationSvc := service_impl.NewCollaborationService(sse)

	handlerFactory := handlersimpl.NewNodeHandlerFactory(engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc, repo.Subscription(), service_impl.NewAuditWriter(repo.Audit()))
	engine.Apply(
		service_impl.WithHandlerFactory(handlerFactory),
		service_impl.WithJobService(jobSvc),
	)

	notificationSvc := service_impl.NewNotificationService(repo.Notification())

	messagingSvc := service_impl.NewMessagingService(engine, externalTaskSvc)
	userSvc := service_impl.NewUserService(repo, "test-jwt-secret")
	groupSvc := service_impl.NewGroupService(repo)
	setupSvc := service_impl.NewSetupService(nil)
	svc := services.NewService(services.ServiceParams{
		OrganizationService:  orgSvc,
		ProjectService:       projectSvc,
		DefinitionService:    defSvc,
		TaskService:          taskSvc,
		ExecutionEngine:      engine,
		JobService:           jobSvc,
		ExternalTaskService:  externalTaskSvc,
		DecisionService:      decisionSvc,
		MigrationService:     migrationSvc,
		ConnectorService:     connectorSvc,
		CollaborationService: collaborationSvc,
		MessagingService:     messagingSvc,
		UserService:          userSvc,
		GroupService:         groupSvc,
		SetupService:         setupSvc,
		NotificationService:  notificationSvc,
	})

	eps := endpoints.MakeEndpoints(svc)
	handler := https.NewHTTPHandler(svc, eps, sse)

	return svc, handler
}

func TestHTTPCreateUser(t *testing.T) {
	ctx := t.Context()
	svc, handler := setupHTTPTestService(t)

	// CreateAuditEntry org first
	org, err := svc.CreateOrganization(ctx, "Test Org", "")
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	// CreateAuditEntry user via service to get a token for auth
	err = svc.CreateUser(ctx, entities.User{
		Organizations: []*entities.Organization{{ID: org.ID}},
		Username:      "admin",
		FullName:      "Admin",
		Email:         "admin@test.com",
		Roles:         []string{"admin"},
	}, "password123")
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	_, token, err := svc.Login(ctx, "admin", "password123")
	if err != nil {
		t.Fatalf("failed to login: %v", err)
	}

	// Now test HTTP create user endpoint
	body := map[string]any{
		"user": map[string]any{
			"organization_id": org.ID.String(),
			"username":        "testuser",
			"full_name":       "Test User",
			"email":           "test@test.com",
			"roles":           []string{"user"},
		},
		"password": "testpass123",
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/users", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)
	t.Logf("CreateUser status: %d", resp.StatusCode)
	t.Logf("CreateUser body: %q", string(respBody))
	t.Logf("CreateUser content-type: %s", resp.Header.Get("Content-Type"))

	// Verify it's valid JSON
	if !json.Valid(respBody) {
		t.Errorf("CreateUser response is not valid JSON: %q", string(respBody))
	}
}

func TestHTTPCreateGroup(t *testing.T) {
	ctx := t.Context()
	svc, handler := setupHTTPTestService(t)

	org, err := svc.CreateOrganization(ctx, "Test Org", "")
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	err = svc.CreateUser(ctx, entities.User{
		Organizations: []*entities.Organization{{ID: org.ID}},
		Username:      "admin",
		FullName:      "Admin",
		Email:         "admin@test.com",
		Roles:         []string{"admin"},
	}, "password123")
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	_, token, err := svc.Login(ctx, "admin", "password123")
	if err != nil {
		t.Fatalf("failed to login: %v", err)
	}

	body := map[string]any{
		"group": map[string]any{
			"name":        "Engineering",
			"description": "Engineering team",
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/organizations/"+org.ID.String()+"/groups", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)
	t.Logf("CreateGroup status: %d", resp.StatusCode)
	t.Logf("CreateGroup body: %q", string(respBody))
	t.Logf("CreateGroup content-type: %s", resp.Header.Get("Content-Type"))

	if !json.Valid(respBody) {
		t.Errorf("CreateGroup response is not valid JSON: %q", string(respBody))
	}
}

func TestHTTPUpdateUser(t *testing.T) {
	ctx := t.Context()
	svc, handler := setupHTTPTestService(t)

	org, err := svc.CreateOrganization(ctx, "Test Org", "")
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	err = svc.CreateUser(ctx, entities.User{
		Organizations: []*entities.Organization{{ID: org.ID}},
		Username:      "admin",
		FullName:      "Admin",
		Email:         "admin@test.com",
		Roles:         []string{"admin"},
	}, "password123")
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	_, token, err := svc.Login(ctx, "admin", "password123")
	if err != nil {
		t.Fatalf("failed to login: %v", err)
	}

	// Read as the organization, which is what the auth interceptor puts on a
	// real request. The user directory is tenant-scoped, so a bare context is
	// answered with nothing once the strict scope is on.
	users, err := svc.ListUsers(
		entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()}), org.ID)
	if err != nil {
		t.Fatalf("failed to list users: %v", err)
	}
	adminUser := users[0]

	body := map[string]any{
		"user": map[string]any{
			"full_name": "Admin Updated",
			"email":     "admin.updated@test.com",
			"roles":     []string{"admin", "manager"},
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/v1/users/"+adminUser.ID.String(), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)
	t.Logf("UpdateUser status: %d", resp.StatusCode)
	t.Logf("UpdateUser body: %q", string(respBody))
	t.Logf("UpdateUser content-type: %s", resp.Header.Get("Content-Type"))

	if !json.Valid(respBody) {
		t.Errorf("UpdateUser response is not valid JSON: %q", string(respBody))
	}
}

func TestConnectRPCListUsers(t *testing.T) {
	ctx := t.Context()
	svc, handler := setupHTTPTestService(t)

	org, err := svc.CreateOrganization(ctx, "Test Org", "")
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	err = svc.CreateUser(ctx, entities.User{
		Organizations: []*entities.Organization{{ID: org.ID}},
		Username:      "admin",
		FullName:      "Admin",
		Email:         "admin@test.com",
		Roles:         []string{"admin"},
	}, "password123")
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	_, token, err := svc.Login(ctx, "admin", "password123")
	if err != nil {
		t.Fatalf("failed to login: %v", err)
	}

	// Simulate Connect RPC call to ListUsers (what the frontend Connect client sends)
	connectBody := `{"organizationId":"` + org.ID.String() + `"}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/process.UserService/ListUsers", bytes.NewReader([]byte(connectBody)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)
	t.Logf("Connect ListUsers status: %d", resp.StatusCode)
	t.Logf("Connect ListUsers body: %q", string(respBody))
	t.Logf("Connect ListUsers content-type: %s", resp.Header.Get("Content-Type"))

	if !json.Valid(respBody) {
		t.Errorf("Connect ListUsers response is not valid JSON: %q", string(respBody))
	}
}

func TestConnectRPCListGroups(t *testing.T) {
	ctx := t.Context()
	svc, handler := setupHTTPTestService(t)

	org, err := svc.CreateOrganization(ctx, "Test Org", "")
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	// Seeded inside the tenant it belongs to, the way a request creates it.
	// Without a tenant on the context the strict scope refuses, and the fixture
	// would fail before the HTTP chain it is testing had run.
	ctx = entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})

	err = svc.CreateGroup(ctx, entities.Group{
		Organization: &entities.Organization{ID: org.ID},
		Name:         "Engineering",
		Description:  "Dev team",
	})
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}

	// 2. Setup admin user for auth
	err = svc.CreateUser(ctx, entities.User{
		Organizations: []*entities.Organization{{ID: org.ID}},
		Username:      "admin",
		FullName:      "Admin",
		Email:         "admin@test.com",
		Roles:         []string{"admin"},
	}, "password123")
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	_, token, err := svc.Login(ctx, "admin", "password123")
	if err != nil {
		t.Fatalf("failed to login: %v", err)
	}

	// 3. Simulate Connect RPC call to ListGroups
	connectBody := `{"organizationId":"` + org.ID.String() + `"}`
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/process.GroupService/ListGroups", bytes.NewReader([]byte(connectBody)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)
	t.Logf("Connect ListGroups status: %d", resp.StatusCode)
	t.Logf("Connect ListGroups body: %q", string(respBody))
	t.Logf("Connect ListGroups content-type: %s", resp.Header.Get("Content-Type"))

	if !json.Valid(respBody) {
		t.Errorf("Connect ListGroups response is not valid JSON: %q", string(respBody))
	}
}

func TestHTTPUpdateGroup(t *testing.T) {
	ctx := t.Context()
	svc, handler := setupHTTPTestService(t)

	org, err := svc.CreateOrganization(ctx, "Test Org", "")
	if err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	err = svc.CreateUser(ctx, entities.User{
		Organizations: []*entities.Organization{{ID: org.ID}},
		Username:      "admin",
		FullName:      "Admin",
		Email:         "admin@test.com",
		Roles:         []string{"admin"},
	}, "password123")
	if err != nil {
		t.Fatalf("failed to create admin: %v", err)
	}

	_, token, err := svc.Login(ctx, "admin", "password123")
	if err != nil {
		t.Fatalf("failed to login: %v", err)
	}

	// Seeded inside the tenant it belongs to, the way a request creates it.
	// Without a tenant on the context the strict scope refuses, and the fixture
	// would fail before the HTTP chain it is testing had run.
	ctx = entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})

	err = svc.CreateGroup(ctx, entities.Group{
		Organization: &entities.Organization{ID: org.ID},
		Name:         "Engineering",
		Description:  "Dev team",
	})
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}

	// Read as the organization, for the same reason the user list above is.
	groups, err := svc.ListGroups(
		entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()}), org.ID)
	if err != nil {
		t.Fatalf("failed to list groups: %v", err)
	}
	group := groups[0]

	body := map[string]any{
		"group": map[string]any{
			"name":        "Engineering Updated",
			"description": "Updated description",
		},
	}
	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/v1/groups/"+group.ID.String(), bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	resp := w.Result()
	respBody, _ := io.ReadAll(resp.Body)
	t.Logf("UpdateGroup status: %d", resp.StatusCode)
	t.Logf("UpdateGroup body: %q", string(respBody))
	t.Logf("UpdateGroup content-type: %s", resp.Header.Get("Content-Type"))

	if !json.Valid(respBody) {
		t.Errorf("UpdateGroup response is not valid JSON: %q", string(respBody))
	}
}
