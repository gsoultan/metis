package user_test

import (
	"bytes"
	"context"
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
	service_impl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/server/repositories/pg"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// orgRolesPassword is every account's password in these worlds.
const orgRolesPassword = "a-password-long-enough"

// orgRolesWorld is an installation of two organizations, Acme and Globex, each
// with a project, served through the production request chain — the chain is
// what decides which organization a request is for, and so which roles count.
type orgRolesWorld struct {
	t       *testing.T
	db      *gorm.DB
	svc     services.ServiceFacade
	handler http.Handler

	acme, globex               uuid.UUID
	acmeProject, globexProject uuid.UUID

	ids    map[string]uuid.UUID
	tokens map[string]string
}

func newOrgRolesWorld(t *testing.T) *orgRolesWorld {
	t.Helper()
	db := testutils.SetupTestDB(t)
	conn := testutils.StormConn(db)
	repo := repositories.NewRepository(conn)
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "organization-roles-test-secret",
		nil, nil, service_impl.NewPlatformUserService(pg.NewPlatformUserRepository(conn)))
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil,
		map[string]health.Checker{}, conn)

	w := &orgRolesWorld{
		t: t, db: db, svc: svc, handler: handler,
		ids: map[string]uuid.UUID{}, tokens: map[string]string{},
	}
	w.acme, w.acmeProject = w.organization("Acme")
	w.globex, w.globexProject = w.organization("Globex")
	return w
}

// organization creates an organization with one project.
func (w *orgRolesWorld) organization(name string) (uuid.UUID, uuid.UUID) {
	w.t.Helper()
	ctx := entities.WithSystemContext(w.t.Context())
	org, err := w.svc.CreateOrganization(ctx, name, "")
	if err != nil {
		w.t.Fatalf("create %s: %v", name, err)
	}
	project, err := w.svc.CreateProject(inOrganization(ctx, org.ID), org.ID, name+" Project", "")
	if err != nil {
		w.t.Fatalf("create %s's project: %v", name, err)
	}
	return org.ID, project.ID
}

// account creates an account in the organizations given, holding global in
// every one of them, and signs it in. Seeded as system work, the way setup
// seeds the first administrator: who may grant what is for the tests to ask.
func (w *orgRolesWorld) account(username string, global []string, organizations ...uuid.UUID) uuid.UUID {
	w.t.Helper()
	account := entities.User{
		ID:       uuid.Must(uuid.NewV7()),
		Username: username,
		FullName: username,
		Email:    username + "@example.com",
		Roles:    global,
	}
	for _, org := range organizations {
		account.Organizations = append(account.Organizations, &entities.Organization{ID: org})
	}
	if err := w.svc.CreateUser(entities.WithSystemContext(w.t.Context()), account, orgRolesPassword); err != nil {
		w.t.Fatalf("create %s: %v", username, err)
	}
	_, token, err := w.svc.Login(w.t.Context(), username, orgRolesPassword)
	if err != nil {
		w.t.Fatalf("sign %s in: %v", username, err)
	}
	w.ids[username] = account.ID
	w.tokens[username] = token
	return account.ID
}

// holdsIn gives an account roles in one organization, straight onto its
// membership: what the account may do there, whoever granted it.
func (w *orgRolesWorld) holdsIn(username string, organization uuid.UUID, roles ...string) {
	w.t.Helper()
	encoded, err := json.Marshal(roles)
	if err != nil {
		w.t.Fatalf("encode %v: %v", roles, err)
	}
	result := w.db.WithContext(w.t.Context()).Exec(
		`UPDATE user_organizations SET roles = ?::jsonb WHERE user_id = ? AND organization_id = ?`,
		string(encoded), w.ids[username], organization)
	if result.Error != nil || result.RowsAffected != 1 {
		w.t.Fatalf("give %s %v in %s: %v (%d rows)", username, roles, organization, result.Error, result.RowsAffected)
	}
}

// call sends a request as somebody, for the organization given — the
// X-Organization-ID header is how a member of several chooses.
func (w *orgRolesWorld) call(as, method, path string, organization uuid.UUID, body any) (int, string) {
	w.t.Helper()
	var payload bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&payload).Encode(body); err != nil {
			w.t.Fatalf("encode the body of %s %s: %v", method, path, err)
		}
	}
	req := httptest.NewRequestWithContext(w.t.Context(), method, path, &payload)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.tokens[as])
	if organization != uuid.Nil {
		req.Header.Set("X-Organization-ID", organization.String())
	}
	rec := httptest.NewRecorder()
	w.handler.ServeHTTP(rec, req)
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

// listEnvironments is an administrator's read in an organization: where its
// project's runtimes live.
func (w *orgRolesWorld) listEnvironments(as string, organization, project uuid.UUID) (int, string) {
	w.t.Helper()
	return w.call(as, http.MethodGet, "/api/v1/environments?project_id="+project.String(), organization, nil)
}

// deploy deploys the smallest process there is into a project, as a designer
// does.
func (w *orgRolesWorld) deploy(as string, organization, project uuid.UUID, key string) (int, string) {
	w.t.Helper()
	return w.call(as, http.MethodPost, "/api/v1/definitions", organization, map[string]any{
		"definition": &entities.ProcessDefinition{
			Project: &entities.Project{ID: project},
			Key:     key,
			Name:    key,
			Nodes: []*entities.Node{
				{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
				{ID: "end", Type: entities.EndEvent, Incoming: []string{"f1"}},
			},
			Flows: []*entities.SequenceFlow{{ID: "f1", SourceRef: "start", TargetRef: "end"}},
		},
	})
}

// inOrganization is a context for the organization given, as the tenant
// resolver leaves a request.
func inOrganization(ctx context.Context, organization uuid.UUID) context.Context {
	return entities.WithTenantContext(ctx, entities.TenantContext{TenantID: organization.String()})
}
