// Package strictscope proves the strict tenant scope can be lived with — not
// at the repository layer, which tests/tenant already covers, but through the
// real entry points: the production HTTP chain built by app.BuildAPIHandler,
// and the job worker started by StartWorkers, which is where system work marks
// itself with WithSystemContext.
//
// This is the coverage .junie/execution-plan.md names as the precondition for
// flipping the strict-tenant-scope default: 106 test call sites enter the
// engine directly and bypass the markers, so a passing suite with the flag
// forced on proves nothing unless it enters the way production traffic does.
// Here every request crosses the auth interceptor, the tenant resolver and the
// endpoint chains, and the background work that advances the process runs on
// the worker's own context — if any of those paths lost its identity, this
// package is what fails.
//
// SQLite only, deliberately: the strict-scope SQL itself is proven per dialect
// by tests/tenant. What this package proves is that identities reach the
// repository from both directions, which does not vary by engine.
package strictscope

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/app"
	"github.com/gsoultan/metis/internal/pkg/features"
	"github.com/gsoultan/metis/internal/pkg/health"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

type harness struct {
	t      *testing.T
	svc    services.ServiceFacade
	server *httptest.Server
}

// newHarness builds the service stack once and serves it through the same
// handler production runs — endpoints, interceptor chain, the lot.
func newHarness(t *testing.T) *harness {
	t.Helper()

	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(db)
	dispatcher := observersimpl.NewEventDispatcher()
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, dispatcher, sse, "strict-scope-test-secret", func(*gorm.DB) {})

	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, db)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &harness{t: t, svc: svc, server: server}
}

// seedOrganization is fixture, not the path under test: an organization, a
// project, and a user who belongs to them. It runs before the strict flag is
// forced on, the way every real installation's data predates the flag.
func (h *harness) seedOrganization(name, username, password string) (orgID, projectID uuid.UUID) {
	h.t.Helper()
	ctx := h.t.Context()

	// Creating the organization carries no tenant, because it is the tenant
	// being created — the genuine bootstrap case.
	org, err := h.svc.CreateOrganization(ctx, name, "")
	if err != nil {
		h.t.Fatalf("create organization %s: %v", name, err)
	}

	// Everything after it does carry one. Not as a convenience: creating a
	// project reads its organization to validate it, and under the strict
	// scope a read with no identity returns nothing — which surfaces here as
	// "record not found" on an organization that plainly exists. In
	// production that call arrives with the tenant the resolver derived from
	// the caller's membership, so this is the same context the real path has,
	// not an exemption from it. Marking the fixture as *system* work instead
	// would have made the suite pass while proving less.
	tenantCtx := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})

	project, err := h.svc.CreateProject(tenantCtx, org.ID, name+" Project", "")
	if err != nil {
		h.t.Fatalf("create project for %s: %v", name, err)
	}

	err = h.svc.CreateUser(tenantCtx, entities.User{
		Username:      username,
		Roles:         []string{entities.RoleAdmin},
		Organizations: []*entities.Organization{{ID: org.ID}},
	}, password)
	if err != nil {
		h.t.Fatalf("create user %s: %v", username, err)
	}
	return org.ID, project.ID
}

// seedUserWithoutOrganization is the unresolvable principal: authenticates
// fine, belongs nowhere.
func (h *harness) seedUserWithoutOrganization(username, password string) {
	h.t.Helper()
	err := h.svc.CreateUser(h.t.Context(), entities.User{
		Username: username,
		Roles:    []string{entities.RoleUser},
	}, password)
	if err != nil {
		h.t.Fatalf("create organization-less user %s: %v", username, err)
	}
}

// do sends one request through the full chain and decodes the response into
// out when a destination is given. It returns the HTTP status.
func (h *harness) do(method, path, token string, body any, out any) int {
	h.t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			h.t.Fatalf("encode %s %s body: %v", method, path, err)
		}
	}
	req, err := http.NewRequestWithContext(h.t.Context(), method, h.server.URL+path, &buf)
	if err != nil {
		h.t.Fatalf("build %s %s: %v", method, path, err)
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
			h.t.Fatalf("decode %s %s response: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

func (h *harness) login(username, password string) string {
	h.t.Helper()
	var resp struct {
		Token string `json:"token"`
	}
	status := h.do(http.MethodPost, "/api/v1/login", "",
		map[string]string{"username": username, "password": password}, &resp)
	if status != http.StatusOK || resp.Token == "" {
		h.t.Fatalf("login as %s: status %d, token %q", username, status, resp.Token)
	}
	return resp.Token
}

// underStrictScope forces the strict tenant scope on for one test and asserts
// it genuinely took effect.
//
// The assertion is the point. Every test in this package passes with the flag
// off as well — they are livability tests, and a permissive scope permits
// everything. So if OverrideForTest ever stopped working, this suite would
// keep passing while proving nothing at all, which is the failure mode this
// repository has shipped twice (a skipped dialect suite, a test tree that
// never ran). The guard turns that silent vacuum into a failure.
func underStrictScope(t *testing.T) {
	t.Helper()
	restore := features.OverrideForTest(features.StrictTenantScope, true)
	t.Cleanup(restore)
	if !features.Enabled(features.StrictTenantScope) {
		t.Fatal("the strict tenant scope did not take effect; this suite would pass without testing anything")
	}
}

// timerThenApproval is the smallest process that needs both identities: the
// timer advances on the job worker (system context), the approval on request
// contexts (tenant).
func timerThenApproval(projectID uuid.UUID, key, assignee string) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     key,
		Name:    "Strict scope drill",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: "wait", Type: entities.IntermediateCatchEvent, Incoming: []string{"f1"}, Outgoing: []string{"f2"},
				Properties: map[string]any{"timer_duration": "PT1S"}},
			{ID: "approve", Name: "Approve", Type: entities.UserTask, Assignee: assignee,
				Incoming: []string{"f2"}, Outgoing: []string{"f3"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f3"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "wait"},
			{ID: "f2", SourceRef: "wait", TargetRef: "approve"},
			{ID: "f3", SourceRef: "approve", TargetRef: "end"},
		},
	}
}

// TestStrictScope_AProcessRunsEndToEndThroughTheRealChain is the affirmative
// case: with the strict scope forced on, a process still deploys, starts,
// waits on a timer the background worker fires, and completes through the
// task inbox — because every path carries an identity. Any background path
// that lost its system marker would strand the instance at the timer, and the
// test would fail waiting for a task that never appears.
func TestStrictScope_AProcessRunsEndToEndThroughTheRealChain(t *testing.T) {
	h := newHarness(t)
	_, projectID := h.seedOrganization("Strict Org", "alice", "correct-horse-battery")

	underStrictScope(t)
	h.svc.StartWorkers(t.Context())

	token := h.login("alice", "correct-horse-battery")

	if status := h.do(http.MethodPost, "/api/v1/definitions", token,
		map[string]any{"definition": timerThenApproval(projectID, "strict-drill", "alice")}, nil); status != http.StatusOK {
		t.Fatalf("deploy definition: status %d", status)
	}

	var started struct {
		InstanceID uuid.UUID `json:"instance_id"`
	}
	if status := h.do(http.MethodPost, "/api/v1/process/start", token,
		map[string]any{"project_id": projectID.String(), "definition_key": "strict-drill"}, &started); status != http.StatusOK {
		t.Fatalf("start process: status %d", status)
	}

	// The timer fires on the worker's poll — background work under strict
	// scope. If the worker's system context were lost, the queries under it
	// would return nothing, the timer would never complete, and this loop is
	// where that failure surfaces.
	taskID := h.waitForTask(token, "Approve")

	// A node with an assignee creates its task already claimed for them, so
	// the assignee completes it directly — claiming again would be refused as
	// a state error, not an authorization one.
	complete := map[string]any{"id": taskID, "user_id": "alice"}
	if status := h.do(http.MethodPost, "/api/v1/tasks/"+taskID+"/complete", token, complete, nil); status != http.StatusOK {
		t.Fatalf("complete task: status %d", status)
	}

	h.waitForInstanceStatus(token, started.InstanceID, "completed")
}

func (h *harness) waitForTask(token, name string) string {
	h.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var resp struct {
			Tasks []struct {
				ID   uuid.UUID `json:"id"`
				Name string    `json:"name"`
			} `json:"tasks"`
		}
		if status := h.do(http.MethodGet, "/api/v1/tasks", token, nil, &resp); status != http.StatusOK {
			h.t.Fatalf("list tasks: status %d", status)
		}
		for _, task := range resp.Tasks {
			if task.Name == name {
				return task.ID.String()
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	h.t.Fatalf("task %q never appeared: the timer job did not complete, which is what a lost system context looks like", name)
	return ""
}

func (h *harness) waitForInstanceStatus(token string, instanceID uuid.UUID, want string) {
	h.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		var resp struct {
			Instance struct {
				Status string `json:"status"`
			} `json:"instance"`
		}
		if status := h.do(http.MethodGet, "/api/v1/instances/"+instanceID.String(), token, nil, &resp); status != http.StatusOK {
			h.t.Fatalf("get instance: status %d", status)
		}
		last = resp.Instance.Status
		if last == want {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	h.t.Fatalf("instance stayed %q, want %q", last, want)
}

// TestStrictScope_RefusesAPrincipalWithNoOrganization is the deny case at the
// front door: a valid login that resolves to no tenant gets a refusal, not an
// unscoped read. Before P0-SEC-05 this exact shape reached every tenant's rows.
func TestStrictScope_RefusesAPrincipalWithNoOrganization(t *testing.T) {
	h := newHarness(t)
	h.seedOrganization("Strict Org", "alice", "correct-horse-battery")
	h.seedUserWithoutOrganization("mallory", "not-a-member-anywhere")

	underStrictScope(t)

	token := h.login("mallory", "not-a-member-anywhere")
	for _, path := range []string{"/api/v1/definitions", "/api/v1/tasks", "/api/v1/instances"} {
		if status := h.do(http.MethodGet, path, token, nil, nil); status == http.StatusOK {
			t.Errorf("GET %s answered 200 to a principal with no organization; absent constraint must mean deny", path)
		}
	}
}

// TestStrictScope_TenantsCannotSeeEachOtherOverHTTP walks the same assertion
// tests/tenant makes at the repository, but through the whole chain: two
// organizations, and reading as one never shows the other's work.
func TestStrictScope_TenantsCannotSeeEachOtherOverHTTP(t *testing.T) {
	h := newHarness(t)
	_, projectA := h.seedOrganization("Org A", "alice", "correct-horse-battery")
	h.seedOrganization("Org B", "bob", "totally-different-org")

	underStrictScope(t)

	aliceToken := h.login("alice", "correct-horse-battery")
	bobToken := h.login("bob", "totally-different-org")

	if status := h.do(http.MethodPost, "/api/v1/definitions", aliceToken,
		map[string]any{"definition": timerThenApproval(projectA, "org-a-only", "alice")}, nil); status != http.StatusOK {
		t.Fatalf("deploy as alice: status %d", status)
	}

	var defs struct {
		Definitions []struct {
			Key string `json:"key"`
		} `json:"definitions"`
	}
	if status := h.do(http.MethodGet, "/api/v1/definitions", bobToken, nil, &defs); status != http.StatusOK {
		t.Fatalf("list definitions as bob: status %d", status)
	}
	for _, def := range defs.Definitions {
		if def.Key == "org-a-only" {
			t.Fatal("bob can see alice's definition through the full chain")
		}
	}
}

// TestJavaScriptConditionWorklist_OverHTTP proves the worklist endpoint the
// javascript-conditions flag points at: authenticated, tenant-scoped, and
// reporting exactly the stored conditions the flag gates.
func TestJavaScriptConditionWorklist_OverHTTP(t *testing.T) {
	h := newHarness(t)
	_, projectA := h.seedOrganization("Org A", "alice", "correct-horse-battery")
	h.seedOrganization("Org B", "bob", "totally-different-org")

	aliceToken := h.login("alice", "correct-horse-battery")
	bobToken := h.login("bob", "totally-different-org")

	const worklistPath = "/api/v1/definitions/javascript-conditions"

	if status := h.do(http.MethodGet, worklistPath, "", nil, nil); status == http.StatusOK {
		t.Fatal("the worklist answered an unauthenticated caller")
	}

	legacy := timerThenApproval(projectA, "legacy-js", "alice")
	legacy.Flows[1].Condition = "js:reviewsDone >= 2"
	if status := h.do(http.MethodPost, "/api/v1/definitions", aliceToken,
		map[string]any{"definition": legacy}, nil); status != http.StatusOK {
		t.Fatalf("deploy legacy definition: status %d", status)
	}

	var worklist struct {
		Usages []struct {
			DefinitionKey string `json:"definition_key"`
			ElementID     string `json:"element_id"`
			Where         string `json:"where"`
		} `json:"usages"`
	}
	if status := h.do(http.MethodGet, worklistPath, aliceToken, nil, &worklist); status != http.StatusOK {
		t.Fatalf("read worklist as alice: status %d", status)
	}
	if len(worklist.Usages) != 1 {
		t.Fatalf("alice's worklist has %d entries, want exactly her one js: condition: %+v", len(worklist.Usages), worklist.Usages)
	}
	if u := worklist.Usages[0]; u.DefinitionKey != "legacy-js" || u.ElementID != "f2" || u.Where != "flow condition" {
		t.Fatalf("worklist mislocated the condition: %+v", u)
	}

	var bobWorklist struct {
		Usages []json.RawMessage `json:"usages"`
	}
	if status := h.do(http.MethodGet, worklistPath, bobToken, nil, &bobWorklist); status != http.StatusOK {
		t.Fatalf("read worklist as bob: status %d", status)
	}
	if len(bobWorklist.Usages) != 0 {
		t.Fatalf("bob's worklist shows another organization's conditions: %s",
			func() string { b, _ := json.Marshal(bobWorklist.Usages); return string(b) }())
	}
}
