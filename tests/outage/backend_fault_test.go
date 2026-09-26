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
	"gorm.io/gorm"
)

// The fault behind TestAWorkerWhoseConnectionIsKilledMidJobFinishesTheJobOnce:
// holding the rows the worker needs, finding the backend that waits on them,
// killing it — and the partner whose first call is held so the test knows when
// the job is mid-call.

// rowHolder is the test's transaction holding the rows the worker will need.
type rowHolder struct {
	tx  *gorm.DB
	pid int64
}

// hold locks each step's row in one transaction of the test's own.
func (d *killDrill) hold(t *testing.T, steps []heldRow) *rowHolder {
	t.Helper()
	tx := d.db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin the holding transaction: %v", tx.Error)
	}
	h := &rowHolder{tx: tx}
	t.Cleanup(func() { tx.Rollback() })
	if err := tx.Raw(`SELECT pg_backend_pid()`).Row().Scan(&h.pid); err != nil {
		t.Fatalf("read the holder's pid: %v", err)
	}
	for _, step := range steps {
		if err := tx.Exec(step.lock, d.instance).Error; err != nil {
			t.Fatalf("hold the row for %q: %v", step.statement, err)
		}
	}
	return h
}

func (h *rowHolder) release(t *testing.T) {
	t.Helper()
	if err := h.tx.Rollback().Error; err != nil {
		t.Fatalf("release the held rows: %v", err)
	}
}

// killTheWorkerAt waits for the worker to block on the row held for step,
// checks it is this test's worker running that step, and terminates its backend.
func (d *killDrill) killTheWorkerAt(t *testing.T, holder *rowHolder, step heldRow) {
	t.Helper()
	type blocked struct {
		PID     int64  `gorm:"column:pid"`
		AppName string `gorm:"column:application_name"`
		Query   string `gorm:"column:query"`
	}
	var waiting blocked
	deadline := time.Now().Add(faultTimeout)
	for waiting.PID == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the worker never reached %q", step.statement)
		}
		var rows []blocked
		if err := d.db.Raw(`SELECT pid, application_name, query FROM pg_stat_activity WHERE ? = ANY(pg_blocking_pids(pid))`,
			holder.pid).Scan(&rows).Error; err != nil {
			t.Fatalf("find the blocked worker: %v", err)
		}
		if len(rows) > 0 {
			waiting = rows[0]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if waiting.AppName != d.appName || !strings.Contains(waiting.Query, step.statement) {
		t.Fatalf("the backend waiting on the held row is %q running %q; want this test's worker running %q",
			waiting.AppName, waiting.Query, step.statement)
	}

	var terminated bool
	if err := d.db.Raw(`SELECT pg_terminate_backend(?)`, waiting.PID).Row().Scan(&terminated); err != nil || !terminated {
		t.Fatalf("terminate the worker's backend %d: %v (terminated=%v)", waiting.PID, err, terminated)
	}
	d.awaitGone(t, waiting.PID)
}

// awaitGone waits until a terminated backend has left pg_stat_activity, so the
// held row is released only once there is nobody left to take it.
func (d *killDrill) awaitGone(t *testing.T, pid int64) {
	t.Helper()
	deadline := time.Now().Add(faultTimeout)
	for {
		var alive int64
		if err := d.db.Raw(`SELECT count(*) FROM pg_stat_activity WHERE pid = ?`, pid).Row().Scan(&alive); err != nil {
			t.Fatalf("watch backend %d: %v", pid, err)
		}
		if alive == 0 {
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
