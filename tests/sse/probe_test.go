package sse_test

import (
	"bufio"
	"context"
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

func TestEventStreamRequiresAToken(t *testing.T) {
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "sse-probe-secret", nil, nil, nil)
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, testutils.StormConn(db))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("connect to the event stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	t.Logf("status=%d", resp.StatusCode)
	if resp.StatusCode == http.StatusOK {
		// Read one event to prove the stream is live, not merely accepted.
		go func() {
			time.Sleep(100 * time.Millisecond)
			sse.BroadcastTo(entities.SSEScope{Organization: uuid.New()}, entities.ProcessEvent{
				Type:      entities.EventProcessStarted,
				Variables: map[string]any{"salary": 120000, "applicant": "Ada Lovelace"},
			})
		}()
		reader := bufio.NewReader(resp.Body)
		line, readErr := reader.ReadString('\n')
		if readErr == nil && strings.Contains(line, "Ada Lovelace") {
			t.Fatalf("an unauthenticated client received another tenant's process variables: %s", strings.TrimSpace(line))
		}
		t.Fatalf("the event stream accepted a request with no token (status %d)", resp.StatusCode)
	}
}
