package bpmn_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/internal/pkg/idempotency"
	"github.com/gsoultan/gobpm/server/domains/entities"
	"github.com/gsoultan/gobpm/server/repositories/models"
)

// A service task's outbound call and the transaction that advances the token
// cannot be the same transaction: one is network I/O, the other takes a row lock
// on the instance, and holding that lock across someone else's API is how one
// slow partner stalls a whole engine.
//
// That separation is the defect. A call that succeeded and then failed to commit
// was retried, and the retry called again. For an endpoint that charges a card,
// that is a second charge — and nothing in the engine knew the first had
// happened.
func TestServiceTask_ACompletedCallIsNotRepeatedByARetry(t *testing.T) {
	var calls atomic.Int64
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"charge_id":"ch_1"}`))
	}))
	defer api.Close()

	h := newServiceTaskHarness(t)
	instance := h.run(t, entities.Node{
		ID:   "charge",
		Name: "Charge the card",
		Type: entities.ServiceTask,
		Properties: map[string]any{
			"http_url":         api.URL,
			"http_method":      "POST",
			"output_charge_id": "chargeId",
			"input_amount":     "amount",
		},
	}, map[string]any{"amount": 100.0})

	if got := calls.Load(); got != 1 {
		t.Fatalf("the first run made %d calls, want one", got)
	}
	if instance.Variables["chargeId"] != "ch_1" {
		t.Fatalf("chargeId = %v, want ch_1 — the call did not land at all", instance.Variables["chargeId"])
	}

	// What a lost commit looks like from the outside: the call happened, and the
	// job row still says there is work to do. The worker picks it up again.
	rewindJobToPending(t, h, instance.ID)
	if err := h.jobSvc.ProcessPendingJobs(t.Context()); err != nil {
		t.Fatalf("retry: %v", err)
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("the retry called the endpoint again — %d calls in total, want one", got)
	}

	// And the work still finishes: the retry reuses the response it recorded
	// rather than skipping the mapping along with the call.
	reloaded, err := h.engine.GetInstance(t.Context(), instance.ID)
	if err != nil {
		t.Fatalf("reload instance: %v", err)
	}
	if reloaded.Variables["chargeId"] != "ch_1" {
		t.Errorf("chargeId = %v after the retry, want ch_1", reloaded.Variables["chargeId"])
	}
}

// The one window this cannot close — the call landed and the record of it did
// not — is closed by the other end, which needs to see the same key twice.
func TestServiceTask_CarriesAKeyDerivedFromTheUnitOfWork(t *testing.T) {
	var seen []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get(idempotency.Header))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer api.Close()

	h := newServiceTaskHarness(t)
	instance := h.run(t, entities.Node{
		ID:         "charge",
		Type:       entities.ServiceTask,
		Properties: map[string]any{"http_url": api.URL, "http_method": "POST"},
	}, map[string]any{"amount": 100.0})

	if len(seen) != 1 || seen[0] == "" {
		t.Fatalf("headers = %v, want one call carrying a key", seen)
	}
	if want := idempotency.ForServiceCall(instance.ID, "charge", ""); seen[0] != want {
		t.Errorf("key = %q, want %q — it must be derived from the unit of work, not the attempt", seen[0], want)
	}
}

// The key identifies a unit of work, which is what makes it survive a restart
// and stay the same across attempts.
func TestIdempotencyKeyIsStableAndPerIteration(t *testing.T) {
	instance := uuid.New()

	first := idempotency.ForServiceCall(instance, "charge", "")
	if again := idempotency.ForServiceCall(instance, "charge", ""); again != first {
		t.Errorf("the key is not stable: %q then %q", first, again)
	}
	if other := idempotency.ForServiceCall(instance, "charge", "item-2"); other == first {
		t.Error("two iterations of one node share a key; each is its own unit of work")
	}
	if other := idempotency.ForServiceCall(uuid.New(), "charge", ""); other == first {
		t.Error("two instances share a key")
	}

	// A node ID comes from a deployed BPMN file, so it is untrusted input of no
	// bounded length. The key is a header value and must stay one.
	long := idempotency.ForServiceCall(instance, string(make([]byte, 8192)), "")
	if len(long) > 64 {
		t.Errorf("key is %d characters; a node ID must not be able to grow it", len(long))
	}
	for _, c := range long {
		if c <= ' ' || c > '~' {
			t.Fatalf("key %q contains a character a header cannot carry", long)
		}
	}
}

// rewindJobToPending puts the instance's job back the way a failed commit leaves
// it: the call made, the work still outstanding.
func rewindJobToPending(t *testing.T, h *serviceTaskHarness, instanceID uuid.UUID) {
	t.Helper()
	jobs, err := h.repo.Job().ListByInstance(t.Context(), instanceID)
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) == 0 {
		t.Fatal("no job was created for the service task")
	}
	job := jobs[len(jobs)-1]
	job.Status = models.JobPending
	job.LockExpires = nil
	job.LockedBy = ""
	if err := h.repo.Job().Update(t.Context(), job); err != nil {
		t.Fatalf("rewind job: %v", err)
	}

	// The recorded call must survive the rewind — it is the only thing that
	// knows the endpoint has already been paid.
	call, err := h.repo.ServiceCall().Get(t.Context(), instanceID, "charge", "")
	if err != nil {
		t.Fatalf("the call was never recorded: %v", err)
	}
	if call.Status != models.ServiceCallCompleted {
		t.Fatalf("recorded call is %q, want completed", call.Status)
	}
}

// The job pool is a fixed number of workers. When one endpoint starts timing
// out, every instance waiting on it takes a worker and holds it for the full
// timeout, and nothing else moves — the human tasks, the timers and the healthy
// integrations all queue behind an outage they have nothing to do with.
//
// After enough consecutive failures the engine stops calling and fails the job
// immediately instead. The instance still fails, and it still retries with
// backoff; it just does it in microseconds rather than a worker-second each.
func TestServiceTask_StopsCallingADownstreamThatKeepsFailing(t *testing.T) {
	var calls atomic.Int64
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer api.Close()

	h := newServiceTaskHarness(t)
	node := entities.Node{
		ID:         "charge",
		Type:       entities.ServiceTask,
		Properties: map[string]any{"http_url": api.URL, "http_method": "POST"},
	}

	// Each instance is its own unit of work, so none of them is skipped for
	// idempotency — every attempt here is a genuine call the breaker has to stop.
	const instances = 12
	for i := 0; i < instances; i++ {
		h.run(t, node, map[string]any{"amount": float64(i)})
	}

	got := calls.Load()
	if got == 0 {
		t.Fatal("the endpoint was never called; the test proves nothing")
	}
	if got >= instances {
		t.Errorf("the endpoint was called %d times for %d instances — the breaker never opened",
			got, instances)
	}
}
