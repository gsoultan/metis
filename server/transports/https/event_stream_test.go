package https

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/observers/impl"
)

func TestTheStreamLimitHoldsTotalAndPerAccountCaps(t *testing.T) {
	limit := newEventStreamLimit(3, 2)

	if got := limit.acquire("ada"); got != streamAllowed {
		t.Fatalf("first stream refused: %v", got)
	}
	if got := limit.acquire("ada"); got != streamAllowed {
		t.Fatalf("second stream refused: %v", got)
	}
	if got := limit.acquire("ada"); got != streamAccountFull {
		t.Fatalf("a third stream for one account should be refused as the account's, got %v", got)
	}
	if got := limit.acquire("bob"); got != streamAllowed {
		t.Fatalf("another account's first stream refused: %v", got)
	}
	if got := limit.acquire("cy"); got != streamServerFull {
		t.Fatalf("a fourth stream in total should be refused as the server's, got %v", got)
	}

	limit.release("ada")
	limit.release("ada")
	limit.release("bob")
	// An account that closes its last stream leaves nothing behind: the map is
	// keyed by account, and must not remember every one that ever connected.
	if len(limit.perAccount) != 0 || limit.open != 0 {
		t.Fatalf("closed streams were still counted: open=%d accounts=%v", limit.open, limit.perAccount)
	}
}

// A quiet stream still says it is there, so a proxy does not cut it and a
// client that has gone is noticed without waiting for an event.
func TestAQuietStreamSendsAHeartbeat(t *testing.T) {
	previous := eventStreamHeartbeat
	eventStreamHeartbeat = 20 * time.Millisecond
	t.Cleanup(func() { eventStreamHeartbeat = previous })

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, EventStreamPath, nil)
	recorder := httptest.NewRecorder()

	serveEventStream(recorder, request, impl.NewSSEObserver())

	if !strings.Contains(recorder.Body.String(), ": keep-alive") {
		t.Fatalf("a quiet stream sent no heartbeat in 200ms: %q", recorder.Body.String())
	}
}
