package decision_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// decisionAPI is the HTTP surface of decisions, served the way the product
// serves it: the real handler, the real authorization chains, and accounts
// that sign in for a token.
type decisionAPI struct {
	server  *httptest.Server
	tokens  map[string]string
	project uuid.UUID
}

const decisionAPIPassword = "decision-api-test-password"

func newDecisionAPI(t *testing.T) *decisionAPI {
	t.Helper()
	db := testutils.SetupTestDB(t)
	conn := testutils.StormConn(db)
	repo := repositories.NewRepository(conn)
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "decision-api-test-secret", nil, nil, nil, func(*gorm.DB) {})
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, conn)
	api := &decisionAPI{server: httptest.NewServer(handler), tokens: map[string]string{}}
	t.Cleanup(api.server.Close)

	org, err := svc.CreateOrganization(context.Background(), "Decision API Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	tctx := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: org.ID.String()})
	project, err := svc.CreateProject(tctx, org.ID, "Decisions", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	api.project = project.ID

	for _, role := range []string{entities.RoleUser, entities.RoleDesigner} {
		name := "decisions-" + role
		if err := svc.CreateUser(tctx, entities.User{
			Username:      name,
			Roles:         []string{role},
			Organizations: []*entities.Organization{{ID: org.ID}},
		}, decisionAPIPassword); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		api.tokens[role] = api.login(t, name)
	}
	return api
}

func (a *decisionAPI) login(t *testing.T, name string) string {
	t.Helper()
	status, body := a.call(t, "", http.MethodPost, "/api/v1/login", map[string]string{"username": name, "password": decisionAPIPassword})
	token, _ := body["token"].(string)
	if status != http.StatusOK || token == "" {
		t.Fatalf("login %s: status %d (%v)", name, status, body)
	}
	return token
}

// call sends one request as the account holding role ("" for nobody) and
// decodes the JSON reply.
func (a *decisionAPI) call(t *testing.T, token, method, path string, body any) (int, map[string]any) {
	t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, a.server.URL+path, payload)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	decoded := map[string]any{}
	if len(raw) > 0 && json.Unmarshal(raw, &decoded) != nil {
		decoded = map[string]any{"body": string(raw)}
	}
	return resp.StatusCode, decoded
}

// as is call as the account holding role.
func (a *decisionAPI) as(t *testing.T, role, method, path string, body any) (int, map[string]any) {
	t.Helper()
	return a.call(t, a.tokens[role], method, path, body)
}

// table is a one-line credit band table in the harness's project, answering
// band for any score over ten.
func (a *decisionAPI) table(band string) map[string]any {
	return a.tableFor("credit-band", "Credit band", band)
}

// tableFor is table under another key and name.
func (a *decisionAPI) tableFor(key, name, band string) map[string]any {
	return map[string]any{
		"project":    map[string]any{"id": a.project.String()},
		"key":        key,
		"name":       name,
		"hit_policy": entities.HitPolicyFirst,
		"inputs":     []any{map[string]any{"id": "in", "label": "Score", "expression": "score", "type": "number"}},
		"outputs":    []any{map[string]any{"id": "out", "label": "Band", "name": "band", "type": "string"}},
		"rules":      []any{map[string]any{"id": "r1", "inputs": []any{"> 10"}, "outputs": []any{band}}},
	}
}

// create stores the first version of credit-band, as a designer, and returns
// its id.
func (a *decisionAPI) create(t *testing.T, band string) string {
	t.Helper()
	return a.createTable(t, a.table(band))
}

// createTable stores table, as a designer, and returns its id.
func (a *decisionAPI) createTable(t *testing.T, table map[string]any) string {
	t.Helper()
	status, body := a.as(t, entities.RoleDesigner, http.MethodPost, "/api/v1/decisions", map[string]any{"decision": table})
	id, _ := body["id"].(string)
	if status != http.StatusOK || id == "" {
		t.Fatalf("create: status %d (%v)", status, body)
	}
	return id
}

// save stores an edit of the version id names, as a designer, and returns the
// reply.
func (a *decisionAPI) save(t *testing.T, id, band string, stage bool) map[string]any {
	t.Helper()
	status, body := a.as(t, entities.RoleDesigner, http.MethodPut, "/api/v1/decisions/"+id,
		map[string]any{"decision": a.table(band), "stage": stage})
	if status != http.StatusOK {
		t.Fatalf("save: status %d (%v)", status, body)
	}
	return body
}

// unpinned evaluates credit-band with no version and returns the band and the
// version that gave it.
func (a *decisionAPI) unpinned(t *testing.T) (string, int) {
	t.Helper()
	status, body := a.as(t, entities.RoleUser, http.MethodPost, "/api/v1/decisions/evaluate", map[string]any{
		"project_id": a.project.String(),
		"key":        "credit-band",
		"variables":  map[string]any{"score": 20},
	})
	if status != http.StatusOK {
		t.Fatalf("evaluate: status %d (%v)", status, body)
	}
	result, _ := body["result"].(map[string]any)
	values, _ := result["values"].(map[string]any)
	band, _ := values["band"].(string)
	version, _ := result["decision_version"].(float64)
	return band, int(version)
}

func (a *decisionAPI) versionsPath() string {
	return "/api/v1/decisions/versions?" + url.Values{"project_id": {a.project.String()}, "key": {"credit-band"}}.Encode()
}
