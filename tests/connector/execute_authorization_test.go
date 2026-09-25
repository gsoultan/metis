package connector_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"gorm.io/gorm"

	"github.com/gsoultan/metis/internal/app"
	"github.com/gsoultan/metis/internal/pkg/health"
	"github.com/gsoultan/metis/server/domains/entities"
	observersimpl "github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/endpoints"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// POST /api/v1/connectors/execute runs a connector with a configuration its
// caller writes — any host, any port. It needed only a login, so every signed-in
// account, down to somebody who only ever opens the task inbox, could make the
// server connect wherever they liked. HTTP calls go through an egress guard; the
// SMTP and AMQP connectors dial directly. That is a way to probe the network
// Metis sits in, from where Metis sits.
//
// Root cause: the endpoint was wired to the chain that proves a caller is signed
// in, not the one that proves who they are.
//
// A plain TCP listener stands in for a host on that network. The test is what the
// listener sees: nothing, for anybody but an administrator.
func TestOnlyAnAdministratorCanMakeTheServerConnectSomewhere(t *testing.T) {
	h := newExecuteHarness(t)
	target := newCountingListener(t)

	for _, role := range []string{entities.RoleUser, entities.RoleOperator, entities.RoleDesigner} {
		t.Run(role, func(t *testing.T) {
			before := target.accepted.Load()
			status, body := h.execute(t, role, target.port)
			if status != http.StatusUnauthorized && status != http.StatusForbidden {
				t.Errorf("a %s was not refused: status %d (%s)", role, status, body)
			}
			if got := target.accepted.Load(); got != before {
				t.Fatalf("a %s made the server open %d connection(s) to %s", role, got-before, target.address)
			}
		})
	}

	// The administrator's connection test on the Connectors page still works:
	// the server reaches the host, which is all this listener can confirm.
	t.Run(entities.RoleAdmin, func(t *testing.T) {
		before := target.accepted.Load()
		h.execute(t, entities.RoleAdmin, target.port)
		if target.accepted.Load() == before {
			t.Fatal("an administrator's connection test no longer reaches the host")
		}
	})
}

type executeHarness struct {
	server *httptest.Server
	tokens map[string]string
}

func newExecuteHarness(t *testing.T) *executeHarness {
	t.Helper()
	db := testutils.SetupTestDB(t)
	conn := testutils.StormConn(db)
	repo := repositories.NewRepository(conn)
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "execute-test-secret", nil, nil, nil, func(*gorm.DB) {})
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, conn)
	h := &executeHarness{server: httptest.NewServer(handler), tokens: map[string]string{}}
	t.Cleanup(h.server.Close)

	org, err := svc.CreateOrganization(context.Background(), "Execute Org", "")
	if err != nil {
		t.Fatalf("create organization: %v", err)
	}
	tctx := entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: org.ID.String()})
	for _, role := range []string{entities.RoleUser, entities.RoleOperator, entities.RoleDesigner, entities.RoleAdmin} {
		name := "execute-" + role
		if err := svc.CreateUser(tctx, entities.User{
			Username:      name,
			Roles:         []string{role},
			Organizations: []*entities.Organization{{ID: org.ID}},
		}, "execute-test-password"); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		h.tokens[role] = h.login(t, name)
	}
	return h
}

// execute asks the server, as somebody holding role, to send an email through
// an SMTP server at 127.0.0.1:port.
func (h *executeHarness) execute(t *testing.T, role string, port int) (int, string) {
	t.Helper()
	return h.post(t, h.tokens[role], "/api/v1/connectors/execute", map[string]any{
		"connector_key": "email-smtp",
		"config":        map[string]any{"host": "127.0.0.1", "port": strconv.Itoa(port), "from": "probe@example.com"},
		"payload":       map[string]any{"to": "nobody@example.com", "subject": "probe", "body": "probe"},
	})
}

func (h *executeHarness) login(t *testing.T, name string) string {
	t.Helper()
	status, body := h.post(t, "", "/api/v1/login", map[string]string{"username": name, "password": "execute-test-password"})
	if status != http.StatusOK {
		t.Fatalf("login %s: status %d (%s)", name, status, body)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil || out.Token == "" {
		t.Fatalf("login %s returned no token: %s", name, body)
	}
	return out.Token
}

func (h *executeHarness) post(t *testing.T, token, path string, body any) (int, string) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, h.server.URL+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(raw)
}

// countingListener is a host on the network that records being reached.
type countingListener struct {
	address  string
	port     int
	accepted atomic.Int64
}

func newCountingListener(t *testing.T) *countingListener {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	tcp, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address is %T", listener.Addr())
	}
	c := &countingListener{address: listener.Addr().String(), port: tcp.Port}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			c.accepted.Add(1)
			_ = conn.Close()
		}
	}()
	return c
}
