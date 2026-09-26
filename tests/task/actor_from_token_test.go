package task_test

import (
	"bytes"
	"context"
	"encoding/json"
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

// TestActorComesFromTheTokenNotTheBody is the regression for the impersonation
// hole: a signed-in member could complete a task assigned to someone else by
// naming that someone in the request body, and the audit trail then recorded
// the named user as the actor. The endpoint now takes the actor from the
// verified token and ignores `user_id`, so the body is powerless.
func TestActorComesFromTheTokenNotTheBody(t *testing.T) {
	h := newTaskHarness(t)

	// alice is the task's assignee; mallory is a fellow org member who is not.
	taskID := h.assignTaskTo(t, "alice")

	// mallory, using her own valid token, tries to complete alice's task by
	// naming alice in the body. The old code compared the body name against
	// the assignee and let it through. It must be refused now.
	status, body := h.post(t, h.tokens["mallory"],
		"/api/v1/tasks/"+taskID+"/complete",
		map[string]any{"user_id": "alice", "variables": map[string]any{"approved": true}})
	if status != http.StatusForbidden {
		t.Fatalf("mallory completing alice's task: got status %d (%s), want 403", status, body)
	}

	// And the task is genuinely untouched.
	if got := h.taskStatus(t, taskID); got != string(entities.TaskClaimed) {
		t.Fatalf("task status after the refused completion: %q, want it still claimed by alice", got)
	}

	// alice, with her own token, completes her own task. The body naming
	// someone else is ignored rather than honoured.
	status, body = h.post(t, h.tokens["alice"],
		"/api/v1/tasks/"+taskID+"/complete",
		map[string]any{"user_id": "mallory", "variables": map[string]any{"approved": true}})
	if status != http.StatusOK {
		t.Fatalf("alice completing her own task: got status %d (%s), want 200", status, body)
	}
	if got := h.taskStatus(t, taskID); got != string(entities.TaskCompleted) {
		t.Fatalf("task status after alice completed it: %q, want completed", got)
	}
}

type taskHarness struct {
	server *httptest.Server
	svc    services.ServiceFacade
	// db is the raw handle, for the tests that have to write a row the way
	// something other than the engine would.
	db     *gorm.DB
	tokens map[string]string
	projID uuid.UUID
	orgID  uuid.UUID
}

func newTaskHarness(t *testing.T) *taskHarness {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "actor-test-secret", nil, nil, nil)

	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, testutils.StormConn(db))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	h := &taskHarness{server: server, svc: svc, db: db, tokens: map[string]string{}}

	ctx := context.Background()
	org, err := svc.CreateOrganization(ctx, "Actor Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	h.orgID = org.ID
	tctx := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})
	project, err := svc.CreateProject(tctx, org.ID, "Actor Project", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	h.projID = project.ID

	for _, name := range []string{"alice", "mallory"} {
		if err := svc.CreateUser(tctx, entities.User{
			Username:      name,
			Roles:         []string{entities.RoleUser},
			Organizations: []*entities.Organization{{ID: org.ID}},
		}, "actor-test-password"); err != nil {
			t.Fatalf("create user %s: %v", name, err)
		}
		h.tokens[name] = h.login(t, name)
	}
	return h
}

func (h *taskHarness) assignTaskTo(t *testing.T, assignee string) string {
	t.Helper()
	ctx := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: h.orgID.String()})
	def := &entities.ProcessDefinition{
		Project: &entities.Project{ID: h.projID},
		Key:     "actor-approval",
		Name:    "Actor approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: "approve", Name: "Approve", Type: entities.UserTask, Assignee: assignee,
				Incoming: []string{"f1"}, Outgoing: []string{"f2"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "end"},
		},
	}
	if _, err := h.svc.CreateDefinition(ctx, def); err != nil {
		t.Fatalf("create definition: %v", err)
	}
	if _, err := h.svc.StartProcess(ctx, h.projID, "actor-approval", nil); err != nil {
		t.Fatalf("start process: %v", err)
	}
	tasks, err := h.svc.ListTasks(ctx, h.projID)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) == 0 {
		t.Fatal("no task was created by the process")
	}
	return tasks[0].ID.String()
}

func (h *taskHarness) login(t *testing.T, name string) string {
	t.Helper()
	status, body := h.post(t, "", "/api/v1/login",
		map[string]string{"username": name, "password": "actor-test-password"})
	if status != http.StatusOK {
		t.Fatalf("login %s: status %d (%s)", name, status, body)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if out.Token == "" {
		t.Fatalf("login %s returned no token: %s", name, body)
	}
	return out.Token
}

func (h *taskHarness) taskStatus(t *testing.T, id string) string {
	t.Helper()
	status, body := h.get(t, h.tokens["alice"], "/api/v1/tasks/"+id)
	if status != http.StatusOK {
		t.Fatalf("get task: status %d (%s)", status, body)
	}
	var out struct {
		Task struct {
			Status string `json:"status"`
		} `json:"task"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	return out.Task.Status
}

func (h *taskHarness) post(t *testing.T, token, path string, body any) (int, string) {
	return h.do(t, http.MethodPost, token, path, body)
}

func (h *taskHarness) get(t *testing.T, token, path string) (int, string) {
	return h.do(t, http.MethodGet, token, path, nil)
}

func (h *taskHarness) do(t *testing.T, method, token, path string, body any) (int, string) {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return resp.StatusCode, out.String()
}
