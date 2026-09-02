package loadtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

// sloHarness serves the same handler production runs, over PostgreSQL.
//
// The handler is the whole point: measuring the service layer directly would
// leave out the interceptor chain, and the tenant scope it applies is exactly
// what turns a list query into a join.
type sloHarness struct {
	t            *testing.T
	db           *gorm.DB
	server       *httptest.Server
	token        string
	orgID        uuid.UUID
	projID       uuid.UUID
	definitionID uuid.UUID

	// lastStatus and lastBytes make a measurement auditable. A read that is
	// fast because it returned an error, or because it returned nothing, is
	// the failure this whole file exists to avoid producing.
	lastStatus int
	lastBytes  int64

	// requests counts issued reads, so each can claim a distinct client
	// address. See get.
	requests int
}

func newSLOHarnessWithService(t *testing.T) (*sloHarness, services.ServiceFacade) {
	t.Helper()

	// 16 connections, so a measurement is not queueing behind itself.
	db := testutils.SetupPostgresDB(t, 16)
	repo := repositories.NewRepository(db)
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "loadtest-secret", func(*gorm.DB) {})

	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, db)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	h := &sloHarness{t: t, db: db, server: server}
	h.seedBaseline(svc)
	return h, svc
}

// seedBaseline creates the tenant the measurements run as.
func (h *sloHarness) seedBaseline(svc services.ServiceFacade) {
	h.t.Helper()
	ctx := h.t.Context()

	org, err := svc.CreateOrganization(ctx, "Load Org", "")
	if err != nil {
		h.t.Fatalf("create organization: %v", err)
	}
	tenantCtx := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})

	project, err := svc.CreateProject(tenantCtx, org.ID, "Load Project", "")
	if err != nil {
		h.t.Fatalf("create project: %v", err)
	}
	if err := svc.CreateUser(tenantCtx, entities.User{
		Username:      "load",
		Roles:         []string{entities.RoleAdmin},
		Organizations: []*entities.Organization{{ID: org.ID}},
	}, "load-test-password"); err != nil {
		h.t.Fatalf("create user: %v", err)
	}

	definitionID, err := svc.CreateDefinition(tenantCtx, approvalDefinition(project.ID))
	if err != nil {
		h.t.Fatalf("create definition: %v", err)
	}

	h.orgID, h.projID, h.definitionID = org.ID, project.ID, definitionID
	h.token = h.login()
}

// seedTenants creates the other organizations whose rows this tenant must not
// see. They are the reason the scope predicate has work to do: measuring
// against a table holding only your own rows measures a filter that never
// filters anything.
func (h *loadHarness) seedTenants(t *testing.T, count int) []uuid.UUID {
	t.Helper()
	projects := []uuid.UUID{h.projID}
	ctx := t.Context()

	svc := h.service
	for i := 1; i < count; i++ {
		org, err := svc.CreateOrganization(ctx, "Neighbour "+uuid.NewString()[:8], "")
		if err != nil {
			t.Fatalf("create neighbour organization: %v", err)
		}
		tenantCtx := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})
		project, err := svc.CreateProject(tenantCtx, org.ID, "Neighbour project", "")
		if err != nil {
			t.Fatalf("create neighbour project: %v", err)
		}
		projects = append(projects, project.ID)
	}
	return projects
}

func (h *sloHarness) login() string {
	h.t.Helper()
	var out struct {
		Token string `json:"token"`
	}
	if code := h.call(http.MethodPost, "/api/v1/login", "",
		map[string]string{"username": "load", "password": "load-test-password"}, &out); code != http.StatusOK {
		h.t.Fatalf("login: status %d", code)
	}
	return out.Token
}

// get issues an authenticated read and discards the body.
//
// The body is read and thrown away rather than left unread: a response the
// client never drains is a connection that cannot be reused, which would make
// this measure connection setup as much as query time.
func (h *sloHarness) get(path string) int {
	h.t.Helper()

	req, err := http.NewRequestWithContext(h.t.Context(), http.MethodGet, h.server.URL+path, nil)
	if err != nil {
		h.t.Fatalf("build %s: %v", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+h.token)

	// Each request comes from its own client address.
	//
	// Not a way around the rate limiter — a way to stop misrepresenting the
	// thing being measured. The limiter allows 240 requests a minute from one
	// address, so a single-client run spends most of its samples timing a 429
	// and reports microseconds that describe a refusal rather than a query.
	// Load is many clients by definition, so this is the accurate shape.
	//
	// It works because the test server listens on loopback, which is a trusted
	// peer, so X-Forwarded-For is believed from it — the same rule production
	// applies to an in-cluster ingress. 198.18.0.0/15 is the range RFC 2544
	// reserves for exactly this.
	h.requests++
	req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.18.%d.%d", (h.requests/254)%254, h.requests%254+1))

	resp, err := h.server.Client().Do(req)
	if err != nil {
		h.t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	n, _ := io.Copy(io.Discard, resp.Body)
	h.lastStatus, h.lastBytes = resp.StatusCode, n
	return resp.StatusCode
}

func (h *sloHarness) call(method, path, token string, body, out any) int {
	h.t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			h.t.Fatalf("encode %s: %v", path, err)
		}
	}
	req, err := http.NewRequestWithContext(h.t.Context(), method, h.server.URL+path, &buf)
	if err != nil {
		h.t.Fatalf("build %s: %v", path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := h.server.Client().Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			h.t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp.StatusCode
}

// approvalDefinition is start → user task → end: one human step, the shape
// almost every real process opens with, and the one that leaves a task behind.
func approvalDefinition(projectID uuid.UUID) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "load-approval",
		Name:    "Load approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: "approve", Name: "Approve", Type: entities.UserTask, Assignee: "load",
				Incoming: []string{"f1"}, Outgoing: []string{"f2"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "end"},
		},
	}
}
