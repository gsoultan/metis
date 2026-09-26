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
// time in the organization being worked in, through
// PUT /api/v1/users/{id}/organization-roles, sending the roles the account is
// to hold there. These hold the answers it shows to that shape of request,
// through the whole production chain: a change is made in this organization
// and nowhere else, and each refusal comes back as a 403 whose words the
// matrix shows as they are.
//
// It used to send PUT /api/v1/users/{id} with the account's own roles, which
// every organization the account belongs to shares — so a tick in one
// organization's matrix granted the role in all of them.

const matrixPassword = "a-password-long-enough"

type matrixWorld struct {
	handler http.Handler
	svc     services.ServiceFacade
	acme    uuid.UUID
	globex  uuid.UUID
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

	w := matrixWorld{handler: handler, svc: svc, tokens: map[string]string{}}
	w.acme = seedOrganization(t, repo, "Acme")
	w.globex = seedOrganization(t, repo, "Globex")
	w.ana = w.seed(t, "ana", "Ana Admin", []string{entities.RoleAdmin}, w.acme)
	w.dana = w.seed(t, "dana", "Dana Scully", []string{entities.RoleDesigner}, w.acme)
	w.sam = w.seed(t, "sam", "Sam Shared", nil, w.acme, w.globex)
	bea := w.seed(t, "bea", "Bea Globex", nil, w.globex)
	w.holds(t, bea, w.globex, entities.RoleAdmin)
	return w
}

// seed creates an account in orgs, holding here in the first of them, and
// signs it in.
func (w matrixWorld) seed(t *testing.T, username, fullName string, here []string, orgs ...uuid.UUID) entities.User {
	t.Helper()
	account := entities.User{
		ID:          uuid.Must(uuid.NewV7()),
		Username:    username,
		FullName:    fullName,
		DisplayName: strings.Fields(fullName)[0],
		Email:       username + "@example.com",
	}
	for _, org := range orgs {
		account.Organizations = append(account.Organizations, &entities.Organization{ID: org})
	}
	ctx := entities.WithSystemContext(t.Context())
	if err := w.svc.CreateUser(ctx, account, matrixPassword); err != nil {
		t.Fatalf("seed %s: %v", username, err)
	}
	if len(here) > 0 {
		w.holds(t, account, orgs[0], here...)
	}
	_, token, err := w.svc.Login(t.Context(), username, matrixPassword)
	if err != nil {
		t.Fatalf("sign %s in: %v", username, err)
	}
	w.tokens[username] = token
	return account
}

func (w matrixWorld) holds(t *testing.T, account entities.User, org uuid.UUID, roles ...string) {
	t.Helper()
	if err := w.svc.SetOrganizationRoles(inOrganization(entities.WithSystemContext(t.Context()), org), account.ID, roles); err != nil {
		t.Fatalf("give %s %v: %v", account.Username, roles, err)
	}
}

// tick sends what one checkbox in the matrix sends: the roles the account is
// to hold in the organization being worked in, with one changed.
func (w matrixWorld) tick(t *testing.T, as string, account entities.User, roles []string) (int, string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"roles": roles})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut,
		"/api/v1/users/"+account.ID.String()+"/organization-roles", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.tokens[as])
	req.Header.Set("X-Organization-ID", w.acme.String())
	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

// stored is the account as a request for org reads it.
func (w matrixWorld) stored(t *testing.T, id, org uuid.UUID) entities.User {
	t.Helper()
	account, err := w.svc.GetUser(inOrganization(entities.WithSystemContext(t.Context()), org), id)
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

func TestATickInTheRoleMatrixChangesTheRoleInThisOrganizationAndNothingElse(t *testing.T) {
	w := newMatrixWorld(t)

	status, body := w.tick(t, "ana", w.dana, []string{entities.RoleDesigner, entities.RoleOperator})
	if status != http.StatusOK {
		t.Fatalf("an administrator granting Operator: got %d (%s), want 200", status, body)
	}
	after := w.stored(t, w.dana.ID, w.acme)
	if !slices.Equal(after.OrganizationRoles, []string{entities.RoleDesigner, entities.RoleOperator}) {
		t.Errorf("roles in Acme after the tick: %v", after.OrganizationRoles)
	}
	if len(after.Roles) != 0 {
		t.Errorf("the tick gave Dana roles in every organization: %v", after.Roles)
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
	if !entities.HasRole(w.stored(t, w.ana.ID, w.acme).OrganizationRoles, entities.RoleAdmin) {
		t.Fatal("the refused change was made anyway")
	}
}

// Sam is in Acme and in Globex. A tick in Acme's matrix is Acme's business
// alone, so Ana needs no say in Globex to make it — and it leaves Sam as he
// was in Globex. It used to be refused outright: the role it granted was
// Sam's in Globex too.
func TestATickGrantsTheRoleInThisOrganizationOnlyEvenToAnAccountAnotherShares(t *testing.T) {
	w := newMatrixWorld(t)

	status, body := w.tick(t, "ana", w.sam, []string{entities.RoleOperator})
	if status != http.StatusOK {
		t.Fatalf("granting Sam Operator in Acme: got %d (%s), want 200", status, body)
	}
	if got := w.stored(t, w.sam.ID, w.acme).OrganizationRoles; !slices.Equal(got, []string{entities.RoleOperator}) {
		t.Errorf("Sam holds %v in Acme, want [OPERATOR]", got)
	}
	inGlobex := w.stored(t, w.sam.ID, w.globex)
	if len(inGlobex.OrganizationRoles) != 0 || len(inGlobex.Roles) != 0 {
		t.Errorf("the tick in Acme reached Globex: %v there, %v everywhere", inGlobex.OrganizationRoles, inGlobex.Roles)
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
	if entities.HasRole(w.stored(t, w.dana.ID, w.acme).OrganizationRoles, entities.RoleAdmin) {
		t.Fatal("a designer made themselves an administrator")
	}
}
