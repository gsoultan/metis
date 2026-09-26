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
)

// The role matrix on the Platform access page grants and revokes one role at a
// time through PUT /api/v1/users/{id}, sending the account's names and email
// as the list holds them beside the new roles. These hold the answers it shows
// to that shape of request, through the whole production chain: a change is
// made without disturbing anything else, and each refusal comes back as a 403
// whose words the matrix shows as they are.

const matrixPassword = "a-password-long-enough"

type matrixWorld struct {
	handler http.Handler
	svc     services.ServiceFacade
	ana     entities.User // Acme's only administrator
	dana    entities.User // a designer at Acme
	sam     entities.User // at Acme and at Globex, where Ana is nobody
	tokens  map[string]string
}

func newMatrixWorld(t *testing.T) matrixWorld {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "role-matrix-test-secret",
		nil, nil, nil)
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil,
		map[string]health.Checker{}, testutils.StormConn(db))

	acme := seedOrganization(t, repo, "Acme")
	globex := seedOrganization(t, repo, "Globex")
	w := matrixWorld{handler: handler, svc: svc, tokens: map[string]string{}}
	w.ana = w.seed(t, "ana", "Ana Admin", []string{entities.RoleAdmin}, acme)
	w.dana = w.seed(t, "dana", "Dana Scully", []string{entities.RoleDesigner}, acme)
	w.sam = w.seed(t, "sam", "Sam Shared", nil, acme, globex)
	w.seed(t, "bea", "Bea Globex", []string{entities.RoleAdmin}, globex)
	return w
}

func (w matrixWorld) seed(t *testing.T, username, fullName string, roles []string, orgs ...uuid.UUID) entities.User {
	t.Helper()
	account := entities.User{
		ID:          uuid.Must(uuid.NewV7()),
		Username:    username,
		FullName:    fullName,
		DisplayName: strings.Fields(fullName)[0],
		Email:       username + "@example.com",
		Roles:       roles,
	}
	for _, org := range orgs {
		account.Organizations = append(account.Organizations, &entities.Organization{ID: org})
	}
	ctx := entities.WithSystemContext(t.Context())
	if err := w.svc.CreateUser(ctx, account, matrixPassword); err != nil {
		t.Fatalf("seed %s: %v", username, err)
	}
	_, token, err := w.svc.Login(t.Context(), username, matrixPassword)
	if err != nil {
		t.Fatalf("sign %s in: %v", username, err)
	}
	w.tokens[username] = token
	return account
}

// tick sends what one checkbox in the matrix sends: the account's names and
// email as they are, and its roles with one changed.
func (w matrixWorld) tick(t *testing.T, as string, account entities.User, roles []string) (int, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"user": map[string]any{
		"id":           account.ID.String(),
		"full_name":    account.FullName,
		"display_name": account.DisplayName,
		"email":        account.Email,
		"roles":        roles,
	}})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/v1/users/"+account.ID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.tokens[as])
	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

func (w matrixWorld) stored(t *testing.T, id uuid.UUID) entities.User {
	t.Helper()
	account, err := w.svc.GetUser(entities.WithSystemContext(t.Context()), id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	return account
}

func refusal(t *testing.T, body string) string {
	t.Helper()
	var reply struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		t.Fatalf("the refusal is not JSON: %q", body)
	}
	return reply.Error
}

func TestATickInTheRoleMatrixChangesTheRoleAndNothingElse(t *testing.T) {
	w := newMatrixWorld(t)

	status, body := w.tick(t, "ana", w.dana, []string{entities.RoleDesigner, entities.RoleOperator})
	if status != http.StatusOK {
		t.Fatalf("an administrator granting Operator: got %d (%s), want 200", status, body)
	}
	after := w.stored(t, w.dana.ID)
	if !slices.Equal(after.Roles, []string{entities.RoleDesigner, entities.RoleOperator}) {
		t.Errorf("roles after the tick: %v", after.Roles)
	}
	if after.FullName != w.dana.FullName || after.DisplayName != w.dana.DisplayName ||
		after.Email != w.dana.Email || after.Username != w.dana.Username {
		t.Errorf("the tick disturbed the rest of the account: %+v", after)
	}
}

func TestTheRoleMatrixIsToldWhyTheLastAdministratorKeepsTheRole(t *testing.T) {
	w := newMatrixWorld(t)

	status, body := w.tick(t, "ana", w.ana, []string{})
	if status != http.StatusForbidden {
		t.Fatalf("the only administrator clearing their own box: got %d (%s), want 403", status, body)
	}
	if want := "forbidden: ana is the last administrator of Acme; make somebody else an administrator first"; refusal(t, body) != want {
		t.Errorf("the refusal reads %q, want %q", refusal(t, body), want)
	}
	if !entities.HasRole(w.stored(t, w.ana.ID).Roles, entities.RoleAdmin) {
		t.Fatal("the refused change was made anyway")
	}
}

func TestTheRoleMatrixIsToldWhyAnAccountAnotherOrganizationSharesCannotChange(t *testing.T) {
	w := newMatrixWorld(t)

	status, body := w.tick(t, "ana", w.sam, []string{entities.RoleOperator})
	if status != http.StatusForbidden {
		t.Fatalf("changing an account Globex shares: got %d (%s), want 403", status, body)
	}
	want := "forbidden: sam also belongs to another organization, which you are not a member of; " +
		"an administrator there has to make this change"
	if refusal(t, body) != want {
		t.Errorf("the refusal reads %q, want %q", refusal(t, body), want)
	}
	if strings.Contains(body, "Globex") {
		t.Errorf("the refusal names an organization Ana is not in: %s", body)
	}
	if len(w.stored(t, w.sam.ID).Roles) != 0 {
		t.Fatal("the refused change was made anyway")
	}
}

// The matrix shows a designer no boxes; the server refuses one anyway.
func TestSomebodyWhoIsNotAnAdministratorCannotChangeRoles(t *testing.T) {
	w := newMatrixWorld(t)

	status, body := w.tick(t, "dana", w.dana, []string{entities.RoleDesigner, entities.RoleAdmin})
	if status != http.StatusForbidden {
		t.Fatalf("a designer granting themselves Administrator: got %d (%s), want 403", status, body)
	}
	if !strings.Contains(refusal(t, body), entities.RoleAdmin) {
		t.Errorf("the refusal %q does not name the role it wants", refusal(t, body))
	}
	if entities.HasRole(w.stored(t, w.dana.ID).Roles, entities.RoleAdmin) {
		t.Fatal("a designer made themselves an administrator")
	}
}
