package sse_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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

// TestOneTenantsEventsDoNotReachAnothersBrowser is the disclosure question.
//
// A ProcessEvent carries the instance's variables — an amount, an applicant's
// name, an approval decision. The stream is a live feed of them, so a client
// registry that does not know who is listening hands every organization's
// business data to every signed-in browser.
func TestOneTenantsEventsDoNotReachAnothersBrowser(t *testing.T) {
	h := newFixture(t)

	stream, done := h.subscribe(t, h.tokenB)
	defer done()

	h.sse.BroadcastTo(entities.SSEScope{Organization: h.orgA}, entities.ProcessEvent{
		Type:      entities.EventProcessStarted,
		Project:   &entities.Project{ID: h.projectA},
		Variables: map[string]any{"applicant": "Ada Lovelace", "salary": 120000},
	})

	if line, ok := stream(500 * time.Millisecond); ok && strings.Contains(line, "Ada Lovelace") {
		t.Fatalf("a browser signed in to organization B received organization A's process variables: %s", strings.TrimSpace(line))
	}
}

// TestATenantsOwnEventsStillReachIt is the other half. Scoping that delivered
// nothing would pass the leak test and break the product.
func TestATenantsOwnEventsStillReachIt(t *testing.T) {
	h := newFixture(t)

	stream, done := h.subscribe(t, h.tokenA)
	defer done()

	h.sse.BroadcastTo(entities.SSEScope{Organization: h.orgA}, entities.ProcessEvent{
		Type:      entities.EventProcessStarted,
		Project:   &entities.Project{ID: h.projectA},
		Variables: map[string]any{"applicant": "Ada Lovelace"},
	})

	line, ok := stream(2 * time.Second)
	if !ok || !strings.Contains(line, "Ada Lovelace") {
		t.Fatalf("a browser signed in to organization A did not receive its own organization's event (got %q)", strings.TrimSpace(line))
	}
}

// TestAnEventFromOneEnvironmentDoesNotReachAnother is the same property along
// the other axis. A browser on the staging port must not see production: giving
// each environment its own database is worth nothing if the live stream carries
// the contents of one to the other.
func TestAnEventFromOneEnvironmentDoesNotReachAnother(t *testing.T) {
	h := newFixture(t)

	// This browser connected on the main port, so its scope names no
	// environment.
	stream, done := h.subscribe(t, h.tokenA)
	defer done()

	h.sse.BroadcastTo(entities.SSEScope{
		Organization: h.orgA,
		Environment:  uuid.MustParse("00000000-0000-0000-0000-0000000000aa"),
	}, entities.ProcessEvent{
		Type:      entities.EventProcessStarted,
		Project:   &entities.Project{ID: h.projectA},
		Variables: map[string]any{"applicant": "Ada Lovelace", "salary": 120000},
	})

	if line, ok := stream(500 * time.Millisecond); ok && strings.Contains(line, "Ada Lovelace") {
		t.Fatalf("an event from a staging runtime reached a browser on the main port: %s", strings.TrimSpace(line))
	}
}

// TestAnUnscopedEventReachesNobody pins the fail-closed reading of an empty
// scope. The alternative reading — "no scope means everybody" — is exactly the
// bug this replaced.
func TestAnUnscopedEventReachesNobody(t *testing.T) {
	h := newFixture(t)

	stream, done := h.subscribe(t, h.tokenA)
	defer done()

	h.sse.BroadcastTo(entities.SSEScope{}, entities.ProcessEvent{
		Type:      entities.EventProcessStarted,
		Variables: map[string]any{"applicant": "Ada Lovelace"},
	})

	if line, ok := stream(500 * time.Millisecond); ok && strings.Contains(line, "Ada Lovelace") {
		t.Fatalf("an event with no audience was delivered anyway: %s", strings.TrimSpace(line))
	}
}

type fixture struct {
	server   *httptest.Server
	sse      *observersimpl.SSEObserver
	tokenB   string
	tokenA   string
	orgA     uuid.UUID
	orgB     uuid.UUID
	projectA uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "sse-leak-secret", nil, nil, nil)
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, testutils.StormConn(db))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	ctx := context.Background()
	orgA, err := svc.CreateOrganization(ctx, "Org A", "")
	if err != nil {
		t.Fatalf("create organization A: %v", err)
	}
	orgB, err := svc.CreateOrganization(ctx, "Org B", "")
	if err != nil {
		t.Fatalf("create organization B: %v", err)
	}
	ctxA := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: orgA.ID.String()})
	ctxB := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: orgB.ID.String()})

	projectA, err := svc.CreateProject(ctxA, orgA.ID, "A", "")
	if err != nil {
		t.Fatalf("create project A: %v", err)
	}
	if err := svc.CreateUser(ctxB, entities.User{
		Username:      "bob",
		Roles:         []string{entities.RoleAdmin},
		Organizations: []*entities.Organization{{ID: orgB.ID}},
	}, "bob-password-1234"); err != nil {
		t.Fatalf("create user in B: %v", err)
	}
	if err := svc.CreateUser(ctxA, entities.User{
		Username:      "ada",
		Roles:         []string{entities.RoleAdmin},
		Organizations: []*entities.Organization{{ID: orgA.ID}},
	}, "ada-password-1234"); err != nil {
		t.Fatalf("create user in A: %v", err)
	}

	f := &fixture{
		server:   server,
		sse:      sse,
		orgA:     orgA.ID,
		orgB:     orgB.ID,
		projectA: projectA.ID,
	}
	f.tokenB = f.login(t, "bob", "bob-password-1234")
	f.tokenA = f.login(t, "ada", "ada-password-1234")
	return f
}

func (f *fixture) login(t *testing.T, username, password string) string {
	t.Helper()
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		t.Fatalf("encode login: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.server.URL+"/api/v1/login", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build login request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if out.Token == "" {
		t.Fatalf("login returned no token (status %d)", resp.StatusCode)
	}
	return out.Token
}

// subscribe opens the event stream and returns a reader for one line.
func (f *fixture) subscribe(t *testing.T, token string) (func(time.Duration) (string, bool), func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.server.URL+"/api/v1/events", nil)
	if err != nil {
		cancel()
		t.Fatalf("build stream request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	// The body is deliberately left open: it is a stream, and the test reads
	// from it for as long as it runs. Closing is registered here rather than
	// deferred so it happens whichever way the test ends.
	//nolint:bodyclose // closed by the cleanup registered on the next line
	resp, err := f.server.Client().Do(req)
	if err != nil {
		cancel()
		t.Fatalf("open the event stream: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		cancel()
		_ = resp.Body.Close()
		t.Fatalf("the event stream refused a valid token: status %d", resp.StatusCode)
	}

	lines := make(chan string, 4)
	go func() {
		reader := bufio.NewReader(resp.Body)
		for {
			line, readErr := reader.ReadString('\n')
			if line != "" {
				lines <- line
			}
			if readErr != nil {
				return
			}
		}
	}()

	// Give the handler a moment to register the client before anything is
	// broadcast; a race here would make the leak look absent.
	time.Sleep(100 * time.Millisecond)

	read := func(wait time.Duration) (string, bool) {
		for deadline := time.After(wait); ; {
			select {
			case line := <-lines:
				if strings.TrimSpace(line) == "" {
					continue
				}
				return line, true
			case <-deadline:
				return "", false
			}
		}
	}
	return read, cancel
}
