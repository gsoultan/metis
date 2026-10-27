// Package people_test drives the People sign-in through the API as the server
// builds it, beside a User of the same installation, to show that each token
// opens its own routes and none of the other's.
package people_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/app"
	"github.com/gsoultan/metis/internal/pkg/health"
	"github.com/gsoultan/metis/internal/pkg/peopletoken"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/metis/tests/testutils"
	"golang.org/x/crypto/bcrypt"
)

const (
	userPassword   = "a-password-long-enough"
	personPassword = "correct horse battery"
)

type world struct {
	handler   http.Handler
	svc       services.ServiceFacade
	repo      repositories.Repository
	conn      *db.Conn
	org       uuid.UUID
	project   uuid.UUID
	userToken string
}

func newWorld(t *testing.T) world {
	t.Helper()
	conn := testutils.SetupTestConn(t)
	repo := repositories.NewRepository(conn)
	sse := observersimpl.NewSSEObserver()
	keys, err := peopletoken.NewKeys("people-key-one-7c1f0e9a4b2d8f63e5a0c9b1d7e3f2a4", "")
	if err != nil {
		t.Fatal(err)
	}
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "people-sign-in-test-secret",
		impl.NewWorkflowUserService(pg.NewWorkflowUserRepository(conn)), nil, nil,
		services.WithPeopleSignIn(impl.NewPeopleSignInService(pg.NewPersonSignInRepository(conn), keys,
			impl.WithPersonAPIKeys(pg.NewPersonAPIKeyRepository(conn)))))
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, conn)

	system := entities.WithSystemContext(t.Context())
	orgID := uuid.Must(uuid.NewV7())
	if err := repo.Organization().Create(system, models.OrganizationModel{
		Base: models.Base{ID: models.UUID(orgID)}, Name: "People Org",
	}); err != nil {
		t.Fatalf("seed the organization: %v", err)
	}
	tenant := entities.WithTenantContext(system, entities.TenantContext{TenantID: orgID.String()})
	project, err := svc.CreateProject(tenant, orgID, "Purchase Approval", "")
	if err != nil {
		t.Fatalf("seed the project: %v", err)
	}

	// Dana runs the platform; Ada does the work. Both are called by a name a
	// process could use, and only one of them is a person.
	if err := svc.CreateUser(system, entities.User{
		ID: uuid.Must(uuid.NewV7()), Username: "dana", Roles: []string{entities.RoleAdmin},
		Organizations: []*entities.Organization{{ID: orgID}},
	}, userPassword); err != nil {
		t.Fatalf("seed dana: %v", err)
	}
	_, userToken, err := svc.Login(t.Context(), "dana", userPassword)
	if err != nil {
		t.Fatalf("sign dana in: %v", err)
	}

	if _, err := pg.NewWorkflowUserRepository(conn).Upsert(system, entities.WorkflowUser{
		Project: &entities.Project{ID: project.ID}, Username: "ada", Email: "ada@example.com", Active: true,
	}); err != nil {
		t.Fatalf("seed ada: %v", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(personPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Main().Exec(t.Context(),
		"UPDATE workflow_users SET password_hash = $1 WHERE username = 'ada'", string(hash)); err != nil {
		t.Fatalf("set ada's password: %v", err)
	}
	return world{handler: handler, svc: svc, repo: repo, conn: conn, org: orgID, project: project.ID, userToken: userToken}
}

func (w world) do(t *testing.T, method, path, token string, body any) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)
	out, _ := io.ReadAll(rec.Body)
	return rec.Code, string(out)
}

func TestAPersonAndAUserEachOpenOnlyTheirOwnRoutes(t *testing.T) {
	w := newWorld(t)

	code, body := w.do(t, http.MethodPost, "/api/v1/people/login", "", map[string]string{
		"project_id": w.project.String(), "username": "ada", "password": personPassword,
	})
	if code != http.StatusOK {
		t.Fatalf("ada signs in: %d %s", code, body)
	}
	var signedIn struct {
		Token  string          `json:"token"`
		Person entities.Person `json:"person"`
	}
	if err := json.Unmarshal([]byte(body), &signedIn); err != nil {
		t.Fatalf("read the sign-in: %v", err)
	}
	if !strings.HasPrefix(signedIn.Token, peopletoken.Prefix) || signedIn.Person.Username != "ada" {
		t.Fatalf("sign-in answered %s", body)
	}
	personToken := signedIn.Token

	if code, body := w.do(t, http.MethodGet, "/api/v1/people/me", personToken, nil); code != http.StatusOK || !strings.Contains(body, `"username":"ada"`) {
		t.Fatalf("ada asks who she is: %d %s", code, body)
	}

	for _, path := range []string{"/api/v1/users/me", "/api/v1/tasks", "/api/v1/participants?project_id=" + w.project.String()} {
		if code, body := w.do(t, http.MethodGet, path, personToken, nil); code != http.StatusUnauthorized {
			t.Errorf("a People token on %s = %d %s, want 401", path, code, body)
		}
	}
	if code, body := w.do(t, http.MethodGet, "/api/v1/people/me", w.userToken, nil); code != http.StatusUnauthorized {
		t.Errorf("a User's token on a People route = %d %s, want 401", code, body)
	}
	if code, _ := w.do(t, http.MethodGet, "/api/v1/users/me", w.userToken, nil); code != http.StatusOK {
		t.Errorf("dana's own token on her own route = %d, want 200", code)
	}

	// A User's password is not a person's: dana is nobody in the directory.
	if code, body := w.do(t, http.MethodPost, "/api/v1/people/login", "", map[string]string{
		"project_id": w.project.String(), "username": "dana", "password": userPassword,
	}); code != http.StatusUnauthorized {
		t.Errorf("a User signing in as a person = %d %s, want 401", code, body)
	}
}
