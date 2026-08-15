package user_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gsoultan/gobpm/server/domains/entities"
	handlersimpl "github.com/gsoultan/gobpm/server/domains/handlers/impl"
	"github.com/gsoultan/gobpm/server/domains/observers/impl"
	"github.com/gsoultan/gobpm/server/domains/services"
	service_impl "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/server/endpoints"
	"github.com/gsoultan/gobpm/server/repositories"
	models2 "github.com/gsoultan/gobpm/server/repositories/models"
	"github.com/gsoultan/gobpm/server/transports/https"
	"gorm.io/gorm"
)

func setupHTTPTestService(t *testing.T) (services.ServiceFacade, http.Handler) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("failed to get sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	err = db.AutoMigrate(
		&models2.OrganizationModel{},
		&models2.ProcessInstanceModel{},
		&models2.TaskModel{},
		&models2.ProcessDefinitionModel{},
		&models2.ProjectModel{},
		&models2.AuditModel{},
		&models2.JobModel{},
		&models2.IncidentModel{},
		&models2.ExternalTaskModel{},
		&models2.Subscription{},
		&models2.DecisionDefinitionModel{},
		&models2.Connector{},
		&models2.ConnectorInstance{},
		&models2.UserModel{},
		&models2.GroupModel{},
		&models2.MembershipModel{},
	)
	if err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	repo := repositories.NewRepository(db)
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
	migrationSvc := service_impl.NewMigrationService(repo)
	sse := impl.NewSSEObserver()
	collaborationSvc := service_impl.NewCollaborationService(sse)

	handlerFactory := handlersimpl.NewNodeHandlerFactory(engine, taskSvc, jobSvc, externalTaskSvc, decisionSvc, connectorSvc, repo.Subscription())
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
	req := httptest.NewRequest("POST", "/api/v1/users", bytes.NewReader(bodyBytes))
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
	req := httptest.NewRequest("POST", "/api/v1/organizations/"+org.ID.String()+"/groups", bytes.NewReader(bodyBytes))
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

	users, err := svc.ListUsers(ctx, org.ID)
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
	req := httptest.NewRequest("PUT", "/api/v1/users/"+adminUser.ID.String(), bytes.NewReader(bodyBytes))
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
	req := httptest.NewRequest("POST", "/api/v1/process.UserService/ListUsers", bytes.NewReader([]byte(connectBody)))
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
	req := httptest.NewRequest("POST", "/api/v1/process.GroupService/ListGroups", bytes.NewReader([]byte(connectBody)))
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

	err = svc.CreateGroup(ctx, entities.Group{
		Organization: &entities.Organization{ID: org.ID},
		Name:         "Engineering",
		Description:  "Dev team",
	})
	if err != nil {
		t.Fatalf("failed to create group: %v", err)
	}

	groups, err := svc.ListGroups(ctx, org.ID)
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
	req := httptest.NewRequest("PUT", "/api/v1/groups/"+group.ID.String(), bytes.NewReader(bodyBytes))
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
