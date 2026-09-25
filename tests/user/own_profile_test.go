package user_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
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

// The Profile page saved through PUT /api/v1/users/{id}, which only an
// administrator may call, so for everybody else saving their own name failed.
// /api/v1/users/me is the self-service pair: the session says whose profile
// it is, and only how the person is named and reached can change through it.

const profilePassword = "a-password-long-enough"

type profileWorld struct {
	handler http.Handler
	repo    repositories.Repository
	orgID   uuid.UUID
	dana    entities.User
	token   string
}

// newProfileWorld has one organization and Dana in it: a designer, signed in,
// and not an administrator.
func newProfileWorld(t *testing.T) profileWorld {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "own-profile-test-secret",
		nil, nil, nil, func(*gorm.DB) {})
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil,
		map[string]health.Checker{}, testutils.StormConn(db))

	w := profileWorld{handler: handler, repo: repo, orgID: seedOrganization(t, repo, "Profile Org")}
	w.dana = entities.User{
		ID:            uuid.Must(uuid.NewV7()),
		Username:      "dana",
		FullName:      "Dana",
		DisplayName:   "Dana",
		Email:         "dana@old.example",
		Roles:         []string{entities.RoleDesigner},
		Organizations: []*entities.Organization{{ID: w.orgID}},
	}
	if err := svc.CreateUser(entities.WithSystemContext(t.Context()), w.dana, profilePassword); err != nil {
		t.Fatalf("seed dana: %v", err)
	}
	_, token, err := svc.Login(t.Context(), "dana", profilePassword)
	if err != nil {
		t.Fatalf("sign dana in: %v", err)
	}
	w.token = token
	return w
}

func (w profileWorld) do(t *testing.T, method, path, token string, body any) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode the request: %v", err)
		}
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

func profileUpdate(fields map[string]any) map[string]any {
	return map[string]any{"user": fields}
}

func TestAnAccountThatIsNotAnAdministratorCanEditItsOwnProfile(t *testing.T) {
	w := newProfileWorld(t)

	status, body := w.do(t, http.MethodPut, "/api/v1/users/me", w.token, profileUpdate(map[string]any{
		"full_name": "Dana Scully", "display_name": "Scully", "email": "dana@example.com",
	}))
	if status != http.StatusOK {
		t.Fatalf("a designer saving their own profile: got %d (%s), want 200", status, body)
	}

	status, body = w.do(t, http.MethodGet, "/api/v1/users/me", w.token, nil)
	if status != http.StatusOK {
		t.Fatalf("reading one's own profile: got %d (%s), want 200", status, body)
	}
	var reply struct {
		User struct {
			ID          uuid.UUID `json:"id"`
			FullName    string    `json:"full_name"`
			DisplayName string    `json:"display_name"`
			Email       string    `json:"email"`
		} `json:"user"`
	}
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		t.Fatalf("decode the profile: %v (%s)", err, body)
	}
	if reply.User.ID != w.dana.ID {
		t.Fatalf("GET /users/me answered with account %s, want the caller's own %s", reply.User.ID, w.dana.ID)
	}
	if reply.User.FullName != "Dana Scully" || reply.User.DisplayName != "Scully" || reply.User.Email != "dana@example.com" {
		t.Fatalf("the saved profile reads back as %+v", reply.User)
	}
}

// Roles are global and decide what somebody may do everywhere they belong,
// so a person editing their own name must not be able to hand themselves a
// role on the way past — nor a username, an organization or somebody else's
// account.
func TestTheOwnProfileChangesNothingButNamesAndEmail(t *testing.T) {
	w := newProfileWorld(t)
	other := seedOrganization(t, w.repo, "Somebody else's organization")

	status, body := w.do(t, http.MethodPut, "/api/v1/users/me", w.token, profileUpdate(map[string]any{
		"full_name":     "Dana Scully",
		"roles":         []string{entities.RoleAdmin},
		"username":      "root",
		"organizations": []map[string]any{{"id": other.String()}},
		"id":            uuid.Must(uuid.NewV7()).String(),
	}))
	if status != http.StatusOK {
		t.Fatalf("saving a profile that also carried roles: got %d (%s), want 200 with the rest ignored", status, body)
	}

	stored, err := w.repo.User().Get(entities.WithSystemContext(t.Context()), w.dana.ID)
	if err != nil {
		t.Fatalf("reload dana: %v", err)
	}
	if !slices.Equal(stored.Roles, []string{entities.RoleDesigner}) {
		t.Fatalf("a profile edit changed the account's roles to %v", stored.Roles)
	}
	if stored.Username != "dana" {
		t.Fatalf("a profile edit changed the username to %q", stored.Username)
	}
	if len(stored.Organizations) != 1 || uuid.UUID(stored.Organizations[0].ID) != w.orgID {
		t.Fatalf("a profile edit changed the organizations to %+v", stored.Organizations)
	}
	if stored.FullName != "Dana Scully" {
		t.Fatalf("the name the edit did carry was not saved: %q", stored.FullName)
	}

	// And the session is no more privileged than before.
	status, body = w.do(t, http.MethodGet, "/api/v1/environments", w.token, nil)
	if status != http.StatusForbidden {
		t.Fatalf("after the edit, an administrator's page answered the designer with %d (%s), want 403", status, body)
	}
}

// PUT /users/{id} stays an administrator's: it can change roles.
func TestEditingAnAccountByIDStaysAdministrative(t *testing.T) {
	w := newProfileWorld(t)

	status, body := w.do(t, http.MethodPut, "/api/v1/users/"+w.dana.ID.String(), w.token, profileUpdate(map[string]any{
		"full_name": "Dana Scully",
	}))
	if status != http.StatusForbidden {
		t.Fatalf("a designer editing an account by id: got %d (%s), want 403", status, body)
	}
}

func TestTheOwnProfileRefusesWhatMailCannotBeSentTo(t *testing.T) {
	w := newProfileWorld(t)

	cases := []struct {
		name   string
		fields map[string]any
	}{
		{"not an address", map[string]any{"email": "dana at example"}},
		{"a name around the address", map[string]any{"email": "Dana <dana@example.com>"}},
		{"a header smuggled after it", map[string]any{"email": "dana@example.com\r\nBcc: all@example.com"}},
		{"a name longer than anybody's", map[string]any{"full_name": strings.Repeat("a", 256)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, body := w.do(t, http.MethodPut, "/api/v1/users/me", w.token, profileUpdate(tc.fields))
			if status != http.StatusBadRequest {
				t.Fatalf("got %d (%s), want 400", status, body)
			}
		})
	}

	stored, err := w.repo.User().Get(entities.WithSystemContext(t.Context()), w.dana.ID)
	if err != nil {
		t.Fatalf("reload dana: %v", err)
	}
	if stored.Email != "dana@old.example" || stored.FullName != "Dana" {
		t.Fatalf("a refused edit was written: email %q, name %q", stored.Email, stored.FullName)
	}
}

func TestTheOwnProfileNeedsSomebodySignedIn(t *testing.T) {
	w := newProfileWorld(t)

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		status, body := w.do(t, method, "/api/v1/users/me", "", profileUpdate(map[string]any{"full_name": "Nobody"}))
		if status != http.StatusUnauthorized {
			t.Fatalf("%s /users/me with no session: got %d (%s), want 401", method, status, body)
		}
	}
}
