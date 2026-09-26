package impl_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/observers/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// Process events are dispatched inside the transaction that produced them,
// and the event webhook posted them there and then — so a transaction that
// rolled back had still told the outside world that the step happened.
func TestTheEventWebhookSendsOnlyWhatCommitted(t *testing.T) {
	var received atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	repo := repositories.NewRepository(testutils.SetupTestConn(t))
	observer := impl.NewWebhookObserver([]string{server.URL}, repo.UnitOfWork().AfterCommit)
	event := entities.ProcessEvent{Type: entities.EventTaskCompleted, Timestamp: time.Now().Unix()}
	ctx := entities.WithSystemContext(t.Context())

	undone := errors.New("the step failed after the event was raised")
	if err := repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		observer.OnEvent(txCtx, event)
		return undone
	}); !errors.Is(err, undone) {
		t.Fatalf("the transaction: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if n := received.Load(); n != 0 {
		t.Fatalf("a rolled-back step was announced %d time(s)", n)
	}

	if err := repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		observer.OnEvent(txCtx, event)
		return nil
	}); err != nil {
		t.Fatalf("the transaction: %v", err)
	}
	for deadline := time.Now().Add(5 * time.Second); received.Load() == 0 && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
	}
	if n := received.Load(); n != 1 {
		t.Fatalf("a committed step was announced %d time(s), want once", n)
	}
}
