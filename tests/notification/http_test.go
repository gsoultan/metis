package notification_test

import (
	"encoding/json"
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

const (
	unreadCountPath = "/api/v1/users/me/notifications/unread-count"
	memberPassword  = "correct-horse-battery"
)

// api serves the bell's reads through the handler production builds: the auth
// interceptor, the tenant resolver and the endpoint chains, so whose
// notifications a request reaches is decided the way it is for real.
type api struct {
	t      *testing.T
	db     *gorm.DB
	svc    services.ServiceFacade
	server *httptest.Server
}

func newAPI(t *testing.T) api {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "notification-test-secret", nil, nil, nil)
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil,
		map[string]health.Checker{}, testutils.StormConn(db))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return api{t: t, db: db, svc: svc, server: server}
}

// organization is an organization with a project, and an account in it for
// each member.
func (a api) organization(members ...string) uuid.UUID {
	a.t.Helper()
	org, err := a.svc.CreateOrganization(a.t.Context(), "Acme", "")
	if err != nil {
		a.t.Fatalf("create the organization: %v", err)
	}
	ctx := entities.WithTenantContext(a.t.Context(), entities.TenantContext{TenantID: org.ID.String()})
	project, err := a.svc.CreateProject(ctx, org.ID, "Acme Project", "")
	if err != nil {
		a.t.Fatalf("create the project: %v", err)
	}
	for _, member := range members {
		if err := a.svc.CreateUser(ctx, entities.User{
			Username:      member,
			Roles:         []string{entities.RoleUser},
			Organizations: []*entities.Organization{{ID: org.ID}},
		}, memberPassword); err != nil {
			a.t.Fatalf("create %s: %v", member, err)
		}
	}
	return project.ID
}

func (a api) signIn(username string) string {
	a.t.Helper()
	var session struct {
		Token string `json:"token"`
	}
	body := `{"username":"` + username + `","password":"` + memberPassword + `"}`
	status := a.send(http.MethodPost, "/api/v1/login", "", body, &session)
	if status != http.StatusOK || session.Token == "" {
		a.t.Fatalf("sign in as %s: status %d", username, status)
	}
	return session.Token
}

// get reads path as whoever token belongs to, decoding a 200 into out.
func (a api) get(path, token string, out any) int {
	a.t.Helper()
	return a.send(http.MethodGet, path, token, "", out)
}

func (a api) send(method, path, token, body string, out any) int {
	a.t.Helper()
	request, err := http.NewRequestWithContext(a.t.Context(), method, a.server.URL+path, strings.NewReader(body))
	if err != nil {
		a.t.Fatalf("build %s %s: %v", method, path, err)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := a.server.Client().Do(request)
	if err != nil {
		a.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()
	if out != nil && response.StatusCode == http.StatusOK {
		if err := json.NewDecoder(response.Body).Decode(out); err != nil {
			a.t.Fatalf("decode %s %s: %v", method, path, err)
		}
	}
	return response.StatusCode
}

// The bell's number is the signed-in person's own, and nothing in the request
// makes it anybody else's.
//
// The list the bell used to count from names its recipient in a user_id on
// the query string. The count takes no such parameter: a user_id added to it
// is not read, and a caller who is not signed in has no count at all.
func TestTheUnreadCountIsTheSignedInPersonsOwn(t *testing.T) {
	a := newAPI(t)
	projectID := a.organization("alice", "bob")
	seedInbox(t, a.db, "alice", projectID, 5, 3)
	seedInbox(t, a.db, "bob", projectID, 4, 4)
	alice := a.signIn("alice")

	for _, path := range []string{unreadCountPath, unreadCountPath + "?user_id=bob"} {
		var answer struct {
			UnreadCount *int64 `json:"unread_count"`
		}
		status := a.get(path, alice, &answer)
		if status != http.StatusOK || answer.UnreadCount == nil {
			t.Fatalf("GET %s as alice: status %d, unread_count %v", path, status, answer.UnreadCount)
		}
		if *answer.UnreadCount != 3 {
			t.Errorf("GET %s as alice counted %d unread; alice has 3", path, *answer.UnreadCount)
		}
	}

	if status := a.get(unreadCountPath, "", nil); status != http.StatusUnauthorized {
		t.Errorf("GET %s with nobody signed in: status %d, want 401", unreadCountPath, status)
	}
}
