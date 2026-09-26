package sse_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
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
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/gsoultan/metis/tests/testutils"
	"gorm.io/gorm"
)

// The live event stream is a request that lasts as long as a tab stays open,
// and it went through the API's backpressure limiter like any other: each open
// tab held one of its 128 in-flight slots, so 128 tabs — across everybody —
// stalled every other call the API answered.

const (
	streamsPerAccount = 16
	accountsWithTabs  = 9 // 9 × 16 = 144 streams, more than the API's 128 slots
)

type streamWorld struct {
	server *httptest.Server
	tokens []string
}

func newStreamWorld(t *testing.T) streamWorld {
	t.Helper()
	db := testutils.SetupTestDB(t)
	repo := repositories.NewRepository(testutils.StormConn(db))
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "sse-backpressure-secret", nil, nil, nil, func(*gorm.DB) {})
	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{}, testutils.StormConn(db))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	system := entities.WithSystemContext(t.Context())
	orgID := uuid.Must(uuid.NewV7())
	if err := repo.Organization().Create(system, models.OrganizationModel{Base: models.Base{ID: models.UUID(orgID)}, Name: "Stream Org"}); err != nil {
		t.Fatalf("seed the organization: %v", err)
	}
	world := streamWorld{server: server}
	for i := range accountsWithTabs {
		username := fmt.Sprintf("viewer-%d", i)
		if err := svc.CreateUser(system, entities.User{
			ID: uuid.Must(uuid.NewV7()), Username: username,
			Organizations: []*entities.Organization{{ID: orgID}},
		}, "a-password-long-enough"); err != nil {
			t.Fatalf("create %s: %v", username, err)
		}
		_, token, err := svc.Login(t.Context(), username, "a-password-long-enough")
		if err != nil {
			t.Fatalf("log %s in: %v", username, err)
		}
		world.tokens = append(world.tokens, token)
	}
	return world
}

// openStream opens one stream and reports its status, holding it open until
// the test ends. A stream the server never answers counts as status 0.
func (w streamWorld) openStream(t *testing.T, token string) int {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	answered, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.server.URL+"/api/v1/events", nil)
	if err != nil {
		// Errorf, not Fatalf: this runs on the test's own goroutines too.
		t.Errorf("build request: %v", err)
		return 0
	}
	req.Header.Set("Authorization", "Bearer "+token)

	// The goroutine that opens the stream owns it: it reports the status and
	// then holds the body open until the test ends, closing it itself — even
	// when the answer arrives after this function stopped waiting for it.
	status := make(chan int, 1)
	go func() {
		resp, err := w.server.Client().Do(req)
		if err != nil {
			status <- 0
			return
		}
		defer func() { _ = resp.Body.Close() }()
		status <- resp.StatusCode
		<-ctx.Done()
	}()
	select {
	case code := <-status:
		return code
	case <-answered.Done():
		return 0
	}
}

func TestOpenEventStreamsDoNotStallTheRestOfTheAPI(t *testing.T) {
	world := newStreamWorld(t)

	var mu sync.Mutex
	opened := 0
	var wg sync.WaitGroup
	for _, token := range world.tokens {
		for range streamsPerAccount {
			wg.Go(func() {
				if world.openStream(t, token) == http.StatusOK {
					mu.Lock()
					opened++
					mu.Unlock()
				}
			})
		}
	}
	wg.Wait()
	if want := accountsWithTabs * streamsPerAccount; opened != want {
		t.Fatalf("only %d of %d streams were served", opened, want)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, world.server.URL+"/api/v1/setup/status", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := world.server.Client().Do(req)
	if err != nil {
		t.Fatalf("with %d tabs open, an ordinary request was not answered: %v", opened, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("with %d tabs open, an ordinary request answered %d", opened, resp.StatusCode)
	}
}

// One account — a script, a tab reloading itself — cannot take every stream.
func TestOneAccountCannotHoldEveryStream(t *testing.T) {
	world := newStreamWorld(t)
	token := world.tokens[0]

	for i := range streamsPerAccount {
		if status := world.openStream(t, token); status != http.StatusOK {
			t.Fatalf("stream %d of %d was refused with %d", i+1, streamsPerAccount, status)
		}
	}
	if status := world.openStream(t, token); status != http.StatusTooManyRequests {
		t.Fatalf("stream %d for one account answered %d, want 429", streamsPerAccount+1, status)
	}
	if status := world.openStream(t, world.tokens[1]); status != http.StatusOK {
		t.Fatalf("another account was refused a stream (%d) because of the first one's", status)
	}
}
