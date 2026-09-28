// Package externaltask_test drives the external-task worker protocol through
// the production request path: the HTTP routes and their Connect twin, behind
// the same sign-in, tenant resolution and error encoding a worker meets.
package externaltask_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
)

const (
	password      = "correct-horse-battery"
	topic         = "charge"
	holder        = "billing-1"
	anotherWorker = "billing-2"
	tenMinutesMS  = int64(10 * time.Minute / time.Millisecond)
	oneDayMS      = int64(24 * time.Hour / time.Millisecond)
)

// world is a server with a charge step waiting for a worker in one
// organization, a member of that organization and a member of another.
type world struct {
	server   *httptest.Server
	member   string // a token of a member of the task's organization
	outsider string // a token of a member of another organization
}

func newWorld(t *testing.T) world {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "extend-lock-test", nil, nil, nil)
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, testutils.StormConn(db))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	billing := organizationWithMember(t, svc, "Billing", "wendy")
	organizationWithMember(t, svc, "Elsewhere", "oscar")

	project, err := svc.CreateProject(billing, tenantOf(t, billing), "Payments", "")
	if err != nil {
		t.Fatalf("create the project: %v", err)
	}
	if _, err := svc.CreateDefinition(billing, &entities.ProcessDefinition{
		Project: &entities.Project{ID: project.ID},
		Key:     "charge-card",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent},
			{ID: "charge", Type: entities.ServiceTask, Name: "Charge the card", ExternalTopic: topic},
			{ID: "end", Type: entities.EndEvent},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "charge"},
			{ID: "f2", SourceRef: "charge", TargetRef: "end"},
		},
	}); err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if _, err := svc.StartProcess(billing, project.ID, "charge-card", nil); err != nil {
		t.Fatalf("start: %v", err)
	}
	return world{server: server, member: signIn(t, server, "wendy"), outsider: signIn(t, server, "oscar")}
}

// organizationWithMember creates an organization and a member of it who may
// sign in, and returns a context acting inside it.
func organizationWithMember(t *testing.T, svc services.ServiceFacade, name, username string) context.Context {
	t.Helper()
	org, err := svc.CreateOrganization(t.Context(), name, "")
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	tenant := entities.WithTenantContext(t.Context(), entities.TenantContext{TenantID: org.ID.String()})
	if err := svc.CreateUser(tenant, entities.User{
		Username: username, Roles: []string{entities.RoleUser}, Organizations: []*entities.Organization{{ID: org.ID}},
	}, password); err != nil {
		t.Fatalf("create %s: %v", username, err)
	}
	return tenant
}

// tenantOf is the organization a context acts inside.
func tenantOf(t *testing.T, ctx context.Context) uuid.UUID {
	t.Helper()
	tc, ok := entities.TenantContextFrom(ctx)
	if !ok {
		t.Fatal("the context carries no tenant")
	}
	return uuid.MustParse(tc.TenantID)
}

func signIn(t *testing.T, server *httptest.Server, username string) string {
	t.Helper()
	status, body := call(t, server, http.MethodPost, "/api/v1/login", "", map[string]string{"username": username, "password": password})
	var session struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &session); err != nil || session.Token == "" {
		t.Fatalf("sign in as %s: status %d, %v (%s)", username, status, err, body)
	}
	return session.Token
}

// call sends one JSON request and returns the status and the body.
func call(t *testing.T, server *httptest.Server, method, path, token string, body any) (int, string) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode the request: %v", err)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	read, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read the reply to %s %s: %v", method, path, err)
	}
	return resp.StatusCode, string(read)
}

// fetch has worker lock the waiting charge step for a minute, over HTTP.
func (w world) fetch(t *testing.T, worker string) string {
	t.Helper()
	status, body := call(t, w.server, http.MethodPost, "/api/v1/external-tasks/fetch-and-lock", w.member, map[string]any{
		"topic": topic, "worker_id": worker, "max_tasks": 1, "lock_duration_ms": 60_000,
	})
	var reply struct {
		Tasks []struct {
			ID string `json:"id"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(body), &reply); err != nil || status != http.StatusOK || len(reply.Tasks) != 1 {
		t.Fatalf("fetch-and-lock: status %d, %v (%s)", status, err, body)
	}
	return reply.Tasks[0].ID
}

func (w world) extend(t *testing.T, taskID, token string, body any) (int, string) {
	t.Helper()
	return call(t, w.server, http.MethodPost, "/api/v1/external-tasks/"+taskID+"/extend-lock", token, body)
}

func TestAWorkerExtendsItsLockOverHTTP(t *testing.T) {
	w := newWorld(t)
	taskID := w.fetch(t, holder)

	asked := time.Now()
	status, body := w.extend(t, taskID, w.member, map[string]any{"worker_id": holder, "lock_duration_ms": tenMinutesMS})
	if status != http.StatusOK {
		t.Fatalf("the holder extending its lock: status %d (%s), want 200", status, body)
	}
	var reply struct {
		LockExpiration time.Time `json:"lock_expiration"`
	}
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		t.Fatalf("decode the reply: %v (%s)", err, body)
	}
	if early, late := asked.Add(10*time.Minute-time.Second), time.Now().Add(10*time.Minute+time.Second); reply.LockExpiration.Before(early) || reply.LockExpiration.After(late) {
		t.Fatalf("the lock now runs out at %v; want ten minutes from now (%s)", reply.LockExpiration, body)
	}
}

// Who and what is refused, and with which status: the worker's own mistakes
// and a lock it no longer holds are 400 — asking again will not help — and
// another organization's task is not found.
func TestAnExtensionTheServerRefusesSaysWhy(t *testing.T) {
	w := newWorld(t)
	taskID := w.fetch(t, holder)

	for _, tc := range []struct {
		name   string
		task   string
		token  string
		body   any
		status int
		words  string
	}{
		{
			name: "another worker", task: taskID, token: w.member,
			body:   map[string]any{"worker_id": anotherWorker, "lock_duration_ms": tenMinutesMS},
			status: http.StatusBadRequest, words: "fetch it again",
		},
		{
			name: "no time at all", task: taskID, token: w.member,
			body:   map[string]any{"worker_id": holder, "lock_duration_ms": 0},
			status: http.StatusBadRequest, words: "lock_duration_ms",
		},
		{
			name: "longer than a day", task: taskID, token: w.member,
			body:   map[string]any{"worker_id": holder, "lock_duration_ms": oneDayMS + 1},
			status: http.StatusBadRequest, words: "lock_duration_ms",
		},
		{
			name: "not a task id", task: "not-a-task", token: w.member,
			body:   map[string]any{"worker_id": holder, "lock_duration_ms": tenMinutesMS},
			status: http.StatusBadRequest, words: "not-a-task",
		},
		{
			name: "another organization's member", task: taskID, token: w.outsider,
			body:   map[string]any{"worker_id": holder, "lock_duration_ms": tenMinutesMS},
			status: http.StatusNotFound, words: "no such external task",
		},
		{
			name: "nobody signed in", task: taskID, token: "",
			body:   map[string]any{"worker_id": holder, "lock_duration_ms": tenMinutesMS},
			status: http.StatusUnauthorized,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := w.extend(t, tc.task, tc.token, tc.body)
			if status != tc.status || !strings.Contains(body, tc.words) {
				t.Fatalf("status %d (%s); want %d saying %q", status, body, tc.status, tc.words)
			}
		})
	}
}
