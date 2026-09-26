package loadtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/idempotency"
)

// The load driver for the write path: it sends what a crowd of people finishing
// work at the same moment sends, and keeps what each request got back.

const (
	// requestTimeout bounds one API call. A write that has not answered by then
	// is waiting on a lock nobody will release, and the test should say so
	// rather than sit out the package timeout.
	requestTimeout = 30 * time.Second
	// responseLimit bounds how much of an answer is read. The API's replies here
	// are a few hundred bytes; a failure keeps its body so the report can quote
	// the error the caller was given.
	responseLimit = 64 << 10
)

// writeClient issues API calls the way a crowd does: concurrently, from many
// client addresses, over connections that stay open.
type writeClient struct {
	baseURL string
	token   string
	http    *http.Client
	// issued numbers requests so each can come from its own address. See
	// sloHarness.get: the limiter allows 240 a minute from one address, and a
	// crowd is many addresses by definition.
	issued atomic.Int64
}

func newWriteClient(h *sloHarness, workers int) *writeClient {
	// The default transport keeps two idle connections per host, so every other
	// worker would dial afresh for each request and the numbers would include
	// connection setup. Pairs put two requests per worker in flight at most.
	transport := &http.Transport{MaxIdleConnsPerHost: 2 * workers}
	return &writeClient{baseURL: h.server.URL, token: h.token, http: &http.Client{Transport: transport}}
}

// apiResult is what one call got back.
type apiResult struct {
	status  int
	body    []byte
	elapsed time.Duration
	// err is set when there was no answer at all.
	err error
}

func (r apiResult) String() string {
	if r.err != nil {
		return "no answer: " + r.err.Error()
	}
	return fmt.Sprintf("%d %s", r.status, bytes.TrimSpace(r.body))
}

// post sends one authenticated JSON request and times it, body included.
func (c *writeClient) post(ctx context.Context, path string, body any) apiResult {
	payload, err := json.Marshal(body)
	if err != nil {
		return apiResult{err: err}
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return apiResult{err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	// 198.19.0.0/16: the half of RFC 2544's benchmarking range the reads in this
	// package do not use.
	n := c.issued.Add(1)
	req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.19.%d.%d", (n/254)%254, n%254+1))

	began := time.Now()
	resp, err := c.http.Do(req)
	if err != nil {
		return apiResult{err: err, elapsed: time.Since(began)}
	}
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, responseLimit))
	return apiResult{status: resp.StatusCode, body: answer, elapsed: time.Since(began), err: err}
}

// startedInstance is one instance and the order number it was started for.
type startedInstance struct {
	order string
	id    uuid.UUID
}

// startInstances starts count instances from workers goroutines at once. Each
// carries its own order number, which is how the partner's calls are matched to
// the instance that made them.
func (c *writeClient) startInstances(ctx context.Context, projectID uuid.UUID, count, workers int) ([]startedInstance, []apiResult) {
	orders := make(chan string)
	var (
		mu      sync.Mutex
		started []startedInstance
		results []apiResult
		wg      sync.WaitGroup
	)
	for range workers {
		wg.Go(func() {
			for order := range orders {
				instance, result := c.startOne(ctx, projectID, order)
				mu.Lock()
				results = append(results, result)
				if instance.id != uuid.Nil {
					started = append(started, instance)
				}
				mu.Unlock()
			}
		})
	}
	for i := range count {
		orders <- fmt.Sprintf("order-%05d", i)
	}
	close(orders)
	wg.Wait()
	return started, results
}

func (c *writeClient) startOne(ctx context.Context, projectID uuid.UUID, order string) (startedInstance, apiResult) {
	result := c.post(ctx, "/api/v1/process/start", map[string]any{
		"project_id":     projectID.String(),
		"definition_key": writeDefinitionKey,
		"variables":      map[string]any{"order": order},
	})
	if result.err != nil || result.status != http.StatusOK {
		return startedInstance{}, result
	}
	var out struct {
		InstanceID uuid.UUID `json:"instance_id"`
	}
	if err := json.Unmarshal(result.body, &out); err != nil {
		result.err = fmt.Errorf("decode the started instance: %w", err)
		return startedInstance{}, result
	}
	return startedInstance{order: order, id: out.InstanceID}, result
}

// move is the completion requests released at one instant: one task, both
// branches of one instance, or the same task twice — a double click, or a
// client resending a request that did not come back in time.
type move []uuid.UUID

// completion is one request to complete a task and what it got back.
type completion struct {
	taskID uuid.UUID
	apiResult
}

// planMoves decides which submissions go together, then shuffles them.
//
// Seeded, so an order that fails can be replayed: the seed is in the log.
func planMoves(rng *rand.Rand, approvals []approval) []move {
	moves := make([]move, 0, 2*len(approvals))
	for _, a := range approvals {
		if rng.IntN(pairOneIn) == 0 {
			moves = append(moves, move{a.finance, a.legal})
			continue
		}
		for _, task := range []uuid.UUID{a.finance, a.legal} {
			if rng.IntN(duplicateOneIn) == 0 {
				moves = append(moves, move{task, task})
				continue
			}
			moves = append(moves, move{task})
		}
	}
	rng.Shuffle(len(moves), func(i, j int) { moves[i], moves[j] = moves[j], moves[i] })
	return moves
}

// gatedRequest is one submission, held until every other submission of its move
// has been picked up by a worker too.
type gatedRequest struct {
	taskID uuid.UUID
	gate   *sync.WaitGroup
}

// completeAll sends every move from workers goroutines.
//
// The submissions of one move are queued next to each other, and each waits at
// its move's gate until all of them have a worker, so they reach the server
// together. That cannot wedge: only the move being queued can be waiting for a
// worker, and with two or more workers one is always free to take it.
func (c *writeClient) completeAll(ctx context.Context, moves []move, workers int, variables func(uuid.UUID) map[string]any) []completion {
	queue := make(chan gatedRequest)
	results := make(chan completion, 2*len(moves))
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for req := range queue {
				req.gate.Done()
				req.gate.Wait()
				path := "/api/v1/tasks/" + req.taskID.String() + "/complete"
				results <- completion{taskID: req.taskID, apiResult: c.post(ctx, path, map[string]any{"variables": variables(req.taskID)})}
			}
		})
	}
	for _, m := range moves {
		gate := &sync.WaitGroup{}
		gate.Add(len(m))
		for _, taskID := range m {
			queue <- gatedRequest{taskID: taskID, gate: gate}
		}
	}
	close(queue)
	wg.Wait()
	close(results)

	all := make([]completion, 0, len(results))
	for r := range results {
		all = append(all, r)
	}
	return all
}

// countingPartner is the downstream the service task calls once both approvals
// are in. It keeps every call by the order it was about and the idempotency key
// it carried, which is how a repeated call is told apart from a new one.
type countingPartner struct {
	server *httptest.Server

	mu          sync.Mutex
	keysByOrder map[string][]string
}

func newCountingPartner(t *testing.T) *countingPartner {
	t.Helper()
	p := &countingPartner{keysByOrder: map[string][]string{}}
	p.server = httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(p.server.Close)
	return p
}

func (p *countingPartner) serve(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Order string `json:"order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Order == "" {
		http.Error(w, "the request names no order", http.StatusBadRequest)
		return
	}
	p.mu.Lock()
	p.keysByOrder[body.Order] = append(p.keysByOrder[body.Order], r.Header.Get(idempotency.Header))
	p.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"receipt": receiptFor(body.Order)})
}

// calls returns a copy of what the partner has been asked, by order.
func (p *countingPartner) calls() map[string][]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string][]string, len(p.keysByOrder))
	for order, keys := range p.keysByOrder {
		out[order] = append([]string(nil), keys...)
	}
	return out
}

// receiptFor is the partner's answer for an order, so the test can tell the
// answer was applied to the instance that asked.
func receiptFor(order string) string { return "receipt-" + order }
