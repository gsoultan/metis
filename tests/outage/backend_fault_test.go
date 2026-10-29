package outage

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gsoultan/metis/internal/pkg/idempotency"
	"github.com/gsoultan/metis/tests/testutils"
	"github.com/gsoultan/storm/pgstat"
	"github.com/gsoultan/storm/runtime"
)

// The fault behind TestAWorkerWhoseConnectionIsKilledMidJobFinishesTheJobOnce:
// holding the rows the worker needs, finding the backend that waits on them,
// killing it — and the partner whose first call is held so the test knows when
// the job is mid-call.

// rowHolder is the test's transaction holding the rows the worker will need.
type rowHolder struct {
	tx  runtime.Tx
	pid int32
}

// hold locks each step's row in one transaction of the test's own.
func (d *killDrill) hold(t *testing.T, steps []heldRow) *rowHolder {
	t.Helper()
	h := &rowHolder{tx: testutils.Begin(t, d.db)}
	pid, _, err := pgstat.Backend(t.Context(), h.tx)
	if err != nil {
		t.Fatalf("read the holder's pid: %v", err)
	}
	h.pid = pid
	for _, step := range steps {
		if err := step.lock(t.Context(), h.tx, d.instance); err != nil {
			t.Fatalf("hold the row the worker needs in %s: %v", step.table, err)
		}
	}
	return h
}

func (h *rowHolder) release(t *testing.T) {
	t.Helper()
	if err := h.tx.Rollback(t.Context()); err != nil {
		t.Fatalf("release the held rows: %v", err)
	}
}

// killTheWorkerAt waits for the worker to block on the row held for step,
// checks it is this test's worker running that step, and terminates its backend.
func (d *killDrill) killTheWorkerAt(t *testing.T, holder *rowHolder, step heldRow) {
	t.Helper()
	ex := testutils.Executor(d.db)
	var waiting pgstat.Session
	deadline := time.Now().Add(faultTimeout)
	for waiting.PID == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the worker never reached %s on %s", step.statement, step.table)
		}
		// This test's own backends only. The held transaction blocks more than
		// the worker: a CREATE INDEX CONCURRENTLY anywhere in the database —
		// another package's migration, in its own schema — waits for every
		// transaction older than it, this one included, so it is blocked by the
		// holder too and can be the first session found.
		sessions, err := pgstat.Sessions(t.Context(), ex)
		if err != nil {
			t.Fatalf("find the blocked worker: %v", err)
		}
		for _, s := range sessions {
			if s.ApplicationName == d.appName && s.WaitsBehind(holder.pid) {
				waiting = s
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	query := strings.ToUpper(strings.TrimSpace(waiting.Query))
	if !strings.HasPrefix(query, step.statement) || !strings.Contains(waiting.Query, step.table) {
		t.Fatalf("the backend waiting on the held row is %q running %q; want this test's worker running %s on %s",
			waiting.ApplicationName, waiting.Query, step.statement, step.table)
	}

	terminated, err := pgstat.Terminate(t.Context(), ex, waiting.PID)
	if err != nil || !terminated {
		t.Fatalf("terminate the worker's backend %d: %v (terminated=%v)", waiting.PID, err, terminated)
	}
	d.awaitGone(t, waiting.PID)
}

// awaitGone waits until a terminated backend has left pg_stat_activity, so the
// held row is released only once there is nobody left to take it.
func (d *killDrill) awaitGone(t *testing.T, pid int32) {
	t.Helper()
	deadline := time.Now().Add(faultTimeout)
	for {
		sessions, err := pgstat.Sessions(t.Context(), testutils.Executor(d.db))
		if err != nil {
			t.Fatalf("watch backend %d: %v", pid, err)
		}
		if !slices.ContainsFunc(sessions, func(s pgstat.Session) bool { return s.PID == pid }) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("backend %d was signalled and is still there", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ---------------------------------------------------------------- the partner

// blockingPartner is the card processor. It holds the first call until the
// test answers it, so the test knows the job is mid-call, and answers any later
// call at once. Every call's idempotency key is kept.
type blockingPartner struct {
	server  *httptest.Server
	arrived chan struct{}
	// answer releases the first call.
	answer func()
	gate   chan struct{}

	mu   sync.Mutex
	keys []string
}

func newBlockingPartner(t *testing.T) *blockingPartner {
	t.Helper()
	p := &blockingPartner{arrived: make(chan struct{}), gate: make(chan struct{})}
	p.answer = sync.OnceFunc(func() { close(p.gate) })
	p.server = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.server.Close)
	// Registered after Close so it runs first: a call still held when the test
	// stops would otherwise keep Close waiting for its client's timeout.
	t.Cleanup(p.answer)
	return p
}

func (p *blockingPartner) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.keys = append(p.keys, r.Header.Get(idempotency.Header))
	first := len(p.keys) == 1
	p.mu.Unlock()

	if first {
		close(p.arrived)
		select {
		case <-p.gate:
		case <-r.Context().Done():
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"charge_id":"ch_1"}`))
}

func (p *blockingPartner) awaitFirstCall(t *testing.T) {
	t.Helper()
	select {
	case <-p.arrived:
	case <-time.After(faultTimeout):
		t.Fatal("the worker never called the partner")
	}
}

func (p *blockingPartner) keysSeen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.keys)
}
