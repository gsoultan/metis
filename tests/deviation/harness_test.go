// Package deviation_test covers the per-instance deviation ledger: what was
// done to one running instance that its process did not decide.
package deviation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

const harnessPassword = "deviation-test-password"

type deviationHarness struct {
	server   *httptest.Server
	svc      services.ServiceFacade
	repo     repositories.Repository
	db       *gorm.DB
	orgID    uuid.UUID
	projID   uuid.UUID
	deployed int
	// seconder is the token of the organization's second administrator, once
	// a test has asked for one (secondAdministrator).
	seconder string
}

func newDeviationHarness(t *testing.T) *deviationHarness {
	t.Helper()
	db := testutils.SetupTestDB(t)
	conn := testutils.StormConn(db)
	repo := repositories.NewRepository(conn)
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "deviation-test-secret", nil, nil, nil)
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, conn)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	org, err := svc.CreateOrganization(context.Background(), "Deviation Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	tctx := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: org.ID.String()})
	project, err := svc.CreateProject(tctx, org.ID, "Deviation Project", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return &deviationHarness{server: server, svc: svc, repo: repo, db: db, orgID: org.ID, projID: project.ID}
}

// routeClient is the client every request to the harness's server is made
// with. It gives up: a request the server never answers fails its test with
// what it was waiting for, where the default client would wait for as long as
// the package is allowed to run.
var routeClient = &http.Client{Timeout: 60 * time.Second}

// tenantContext is a request from inside the harness's organization.
func (h *deviationHarness) tenantContext() context.Context {
	return entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: h.orgID.String()})
}

// signIn creates an account in the harness's organization and returns its token.
func (h *deviationHarness) signIn(t *testing.T, name string, roles ...string) string {
	t.Helper()
	return h.signInTo(t, h.orgID, name, roles...)
}

// signInElsewhere creates an account in a second organization of its own.
func (h *deviationHarness) signInElsewhere(t *testing.T, name string, roles ...string) string {
	t.Helper()
	org, err := h.svc.CreateOrganization(context.Background(), "Elsewhere "+name, "")
	if err != nil {
		t.Fatalf("create the other organization: %v", err)
	}
	return h.signInTo(t, org.ID, name, roles...)
}

func (h *deviationHarness) signInTo(t *testing.T, orgID uuid.UUID, name string, roles ...string) string {
	t.Helper()
	tctx := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: orgID.String()})
	if err := h.svc.CreateUser(tctx, entities.User{
		Username: name, Roles: roles, Organizations: []*entities.Organization{{ID: orgID}},
	}, harnessPassword); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	status, body := h.do(t, http.MethodPost, "", "/api/v1/login", map[string]string{"username": name, "password": harnessPassword})
	if status != http.StatusOK {
		t.Fatalf("login %s: %d (%s)", name, status, body)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.Token == "" {
		t.Fatalf("login %s returned no token: %s", name, body)
	}
	return out.Token
}

func (h *deviationHarness) do(t *testing.T, method, token, path string, body any) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req, err := http.NewRequestWithContext(context.Background(), method, h.server.URL+path, &buf)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := routeClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return resp.StatusCode, out.String()
}

// startOneStep deploys start → step → end under a key of its own, starts it
// and returns the instance.
func (h *deviationHarness) startOneStep(t *testing.T, step entities.Node) uuid.UUID {
	t.Helper()
	h.deployed++
	key := fmt.Sprintf("deviation-one-step-%d", h.deployed)
	step.ID, step.Incoming, step.Outgoing = "step", []string{"f1"}, []string{"f2"}
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID}, Key: key, Name: "One step",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			&step,
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "step"},
			{ID: "f2", SourceRef: "step", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(h.tenantContext(), def); err != nil {
		t.Fatalf("deploy %s: %v", key, err)
	}
	id, err := h.svc.StartProcess(h.tenantContext(), h.projID, key, nil)
	if err != nil {
		t.Fatalf("start %s: %v", key, err)
	}
	return id
}

// write records d through the repository inside a unit of work of its own.
func (h *deviationHarness) write(ctx context.Context, d entities.Deviation) (entities.Deviation, error) {
	var written entities.Deviation
	err := h.repo.UnitOfWork().Do(ctx, func(tx context.Context) error {
		var err error
		written, err = h.repo.Deviation().Create(tx, d)
		return err
	})
	return written, err
}

// sample is a well-formed non-holder reassignment on instanceID.
func (h *deviationHarness) sample(instanceID uuid.UUID) entities.Deviation {
	return entities.Deviation{
		ID:       uuid.Must(uuid.NewV7()),
		Project:  &entities.Project{ID: h.projID},
		Instance: &entities.ProcessInstance{ID: instanceID},
		Kind:     entities.DeviationReassign,
		Scope:    entities.DeviationScopeTask,
		Origin:   entities.DeviationOriginTask,
		Status:   entities.DeviationApplied,
		Node:     &entities.Node{ID: "step", Name: "Approve"},
		Actor:    "boss",
		Reason:   "alice is on leave",
		RunID:    uuid.Must(uuid.NewV7()),
		Before:   map[string]any{"tasks": map[string]any{"t1": map[string]any{"assignee": "alice"}}},
		After:    map[string]any{"tasks": map[string]any{"t1": map[string]any{"assignee": "bob"}}},
	}
}

// rowCount is how many ledger rows instanceID has, read straight from the table.
func (h *deviationHarness) rowCount(t *testing.T, instanceID uuid.UUID) int {
	t.Helper()
	var n int
	if err := h.db.Raw(`SELECT count(*) FROM instance_deviations WHERE instance_id = ?`, instanceID).Row().Scan(&n); err != nil {
		t.Fatalf("count the ledger rows: %v", err)
	}
	return n
}
