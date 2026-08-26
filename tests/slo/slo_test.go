// Package slo turns the roadmap's §1 targets into something that fails.
//
// Those numbers — p95 under 150ms for reads, under 500ms for workflow actions,
// under 0.1% 5xx — are written down as non-negotiable, and until this package
// existed nothing measured any of them. A target nobody measures is a wish, and
// the metrics histogram was built with buckets straddling exactly these
// thresholds for a reader that did not exist.
//
// **Why asserting real production targets here is not flaky.** These figures
// are for production hardware across a network. This test drives the same
// handler in-process, so a read that must beat 150ms actually completes in
// single-digit milliseconds — one to two orders of magnitude of headroom. The
// assertion therefore does not measure the machine it runs on; it catches the
// class of regression that eats an order of magnitude, which is what an N+1
// query or a per-request compile does. If these ever fail on a developer's
// laptop, something is genuinely wrong.
//
// It runs against whatever database is configured, SQLite by default, and takes
// GOBPM_TEST_POSTGRES_DSN when set — see AGENTS.md §4.
package slo

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/gobpm/internal/app"
	"github.com/gsoultan/gobpm/internal/pkg/health"
	"github.com/gsoultan/gobpm/server/domains/entities"
	observersimpl "github.com/gsoultan/gobpm/server/domains/observers/impl"
	"github.com/gsoultan/gobpm/server/domains/services"
	"github.com/gsoultan/gobpm/server/endpoints"
	"github.com/gsoultan/gobpm/server/repositories"
	"github.com/gsoultan/gobpm/tests/testutils"
	"gorm.io/gorm"
)

// The targets, from .junie/roadmap.md §1.
const (
	readP95Target   = 150 * time.Millisecond
	actionP95Target = 500 * time.Millisecond
	errorBudget     = 0.001 // 0.1% of responses may be 5xx
)

type harness struct {
	t      *testing.T
	server *httptest.Server
	token  string
	orgID  uuid.UUID
	projID uuid.UUID
}

func openDB(t *testing.T) *gorm.DB {
	t.Helper()
	// A real server database gives real concurrency; SQLite is capped at one
	// connection here, which is correct for SQLite and does bound throughput.
	// The latency targets hold either way, which is the point.
	if os.Getenv(testutils.PostgresDSNEnv) != "" {
		return testutils.SetupPostgresDB(t, 16)
	}
	return testutils.SetupTestDB(t)
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	db := openDB(t)
	repo := repositories.NewRepository(db)
	sse := observersimpl.NewSSEObserver()
	svc := services.NewServiceFacade(repo, observersimpl.NewEventDispatcher(), sse, "slo-test-secret", func(*gorm.DB) {})

	handler, _ := app.BuildAPIHandler(svc, endpoints.MakeEndpoints(svc), sse, nil, map[string]health.Checker{})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	h := &harness{t: t, server: server}
	h.seed(svc)
	return h
}

func (h *harness) seed(svc services.ServiceFacade) {
	h.t.Helper()
	ctx := h.t.Context()

	org, err := svc.CreateOrganization(ctx, "SLO Org", "")
	if err != nil {
		h.t.Fatalf("create organization: %v", err)
	}
	tenantCtx := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})
	project, err := svc.CreateProject(tenantCtx, org.ID, "SLO Project", "")
	if err != nil {
		h.t.Fatalf("create project: %v", err)
	}
	if err := svc.CreateUser(tenantCtx, entities.User{
		Username:      "load",
		Roles:         []string{entities.RoleAdmin},
		Organizations: []*entities.Organization{{ID: org.ID}},
	}, "load-test-password"); err != nil {
		h.t.Fatalf("create user: %v", err)
	}
	h.orgID, h.projID = org.ID, project.ID
	h.token = h.login()

	if _, err := svc.CreateDefinition(tenantCtx, approvalDefinition(project.ID)); err != nil {
		h.t.Fatalf("create definition: %v", err)
	}
}

// approvalDefinition is start → user task → end: one human step, which is the
// shape almost every real process opens with.
func approvalDefinition(projectID uuid.UUID) *entities.ProcessDefinition {
	return &entities.ProcessDefinition{
		Project: &entities.Project{ID: projectID},
		Key:     "slo-approval",
		Name:    "SLO approval",
		Nodes: []*entities.Node{
			{ID: "start", Type: entities.StartEvent, Outgoing: []string{"f1"}},
			{ID: "approve", Name: "Approve", Type: entities.UserTask, Assignee: "load",
				Incoming: []string{"f1"}, Outgoing: []string{"f2"}},
			{ID: "end", Type: entities.EndEvent, Incoming: []string{"f2"}},
		},
		Flows: []*entities.SequenceFlow{
			{ID: "f1", SourceRef: "start", TargetRef: "approve"},
			{ID: "f2", SourceRef: "approve", TargetRef: "end"},
		},
	}
}

func (h *harness) login() string {
	h.t.Helper()
	var out struct {
		Token string `json:"token"`
	}
	if code := h.call(http.MethodPost, "/api/v1/login", "",
		map[string]string{"username": "load", "password": "load-test-password"}, &out); code != http.StatusOK {
		h.t.Fatalf("login: status %d", code)
	}
	return out.Token
}

// call issues one request and returns its status code.
func (h *harness) call(method, path, token string, body, out any) int {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			h.t.Fatalf("encode %s: %v", path, err)
		}
	}
	req, err := http.NewRequestWithContext(h.t.Context(), method, h.server.URL+path, &buf)
	if err != nil {
		h.t.Fatalf("build %s: %v", path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := h.server.Client().Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			h.t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp.StatusCode
}

// sample is one measured run of one operation.
type sample struct {
	elapsed time.Duration
	status  int
}

// report holds a measured population and answers the questions the SLOs ask.
type report struct {
	name     string
	samples  []sample
	duration time.Duration
}

func (r report) percentile(p float64) time.Duration {
	if len(r.samples) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(r.samples))
	for i, s := range r.samples {
		sorted[i] = s.elapsed
	}
	slices.Sort(sorted)
	// Nearest-rank: the smallest value at or above which p of the population
	// falls. No interpolation, so the number reported is one that was actually
	// observed rather than one between two that were.
	index := int(float64(len(sorted)-1) * p)
	return sorted[index]
}

func (r report) serverErrorRate() float64 {
	if len(r.samples) == 0 {
		return 0
	}
	var failures int
	for _, s := range r.samples {
		if s.status >= 500 {
			failures++
		}
	}
	return float64(failures) / float64(len(r.samples))
}

func (r report) throughputPerMinute() float64 {
	if r.duration <= 0 {
		return 0
	}
	return float64(len(r.samples)) / r.duration.Minutes()
}

func (r report) log(t *testing.T) {
	t.Helper()
	t.Logf("%s: n=%d  p50=%v  p95=%v  p99=%v  max=%v  5xx=%.3f%%  %.0f/min",
		r.name, len(r.samples),
		r.percentile(0.50).Round(time.Microsecond),
		r.percentile(0.95).Round(time.Microsecond),
		r.percentile(0.99).Round(time.Microsecond),
		r.percentile(1.0).Round(time.Microsecond),
		r.serverErrorRate()*100,
		r.throughputPerMinute())
}

// drive runs op concurrently until it has collected count samples.
func drive(t *testing.T, name string, workers, count int, op func(worker int) sample) report {
	t.Helper()

	var (
		mu      sync.Mutex
		samples = make([]sample, 0, count)
		issued  atomic.Int64
	)

	start := time.Now()
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for issued.Add(1) <= int64(count) {
				s := op(w)
				mu.Lock()
				samples = append(samples, s)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	r := report{name: name, samples: samples, duration: time.Since(start)}
	r.log(t)
	return r
}

// TestReadLatencyMeetsItsTarget covers "common reads": the list endpoints an
// operator's screen refreshes against.
func TestReadLatencyMeetsItsTarget(t *testing.T) {
	h := newHarness(t)

	// Give the caller something to read, so the measurement is not of empty
	// tables — an index answers a query over no rows far too quickly to mean
	// anything.
	for range 25 {
		if code := h.call(http.MethodPost, "/api/v1/process/start", h.token,
			map[string]any{"project_id": h.projID.String(), "definition_key": "slo-approval"}, nil); code != http.StatusOK {
			t.Fatalf("seeding instances: status %d", code)
		}
	}

	// Each carries the parameters a real client sends. An earlier version of
	// this test called /instances with no project_id and measured a 500 as
	// though it were a read — which is how the missing 400 mapping was found,
	// but is not a latency measurement of anything.
	project := h.projID.String()
	paths := []string{
		"/api/v1/tasks",
		"/api/v1/instances?project_id=" + project,
		"/api/v1/definitions?project_id=" + project,
	}

	// Warm up: first-call costs (statement preparation, lazily built caches)
	// are real but they are not what a steady-state percentile describes.
	for _, p := range paths {
		h.call(http.MethodGet, p, h.token, nil, nil)
	}

	r := drive(t, "reads", 8, 400, func(worker int) sample {
		path := paths[worker%len(paths)]
		began := time.Now()
		status := h.call(http.MethodGet, path, h.token, nil, nil)
		return sample{elapsed: time.Since(began), status: status}
	})

	if p95 := r.percentile(0.95); p95 > readP95Target {
		t.Errorf("read p95 is %v, over the %v target — in-process this has two orders of magnitude of headroom, so exceeding it means a real regression",
			p95.Round(time.Microsecond), readP95Target)
	}
	if rate := r.serverErrorRate(); rate > errorBudget {
		t.Errorf("read 5xx rate is %.3f%%, over the %.1f%% budget", rate*100, errorBudget*100)
	}
}

// TestWorkflowActionLatencyMeetsItsTarget covers the 500ms target: starting an
// instance is a write, a token advance and an event dispatch, which is the
// heaviest thing an ordinary user does synchronously.
func TestWorkflowActionLatencyMeetsItsTarget(t *testing.T) {
	h := newHarness(t)

	h.call(http.MethodPost, "/api/v1/process/start", h.token,
		map[string]any{"project_id": h.projID.String(), "definition_key": "slo-approval"}, nil)

	body := map[string]any{"project_id": h.projID.String(), "definition_key": "slo-approval"}
	r := drive(t, "process starts", 8, 200, func(int) sample {
		began := time.Now()
		status := h.call(http.MethodPost, "/api/v1/process/start", h.token, body, nil)
		return sample{elapsed: time.Since(began), status: status}
	})

	if p95 := r.percentile(0.95); p95 > actionP95Target {
		t.Errorf("workflow action p95 is %v, over the %v target", p95.Round(time.Microsecond), actionP95Target)
	}
	if rate := r.serverErrorRate(); rate > errorBudget {
		t.Errorf("action 5xx rate is %.3f%%, over the %.1f%% budget", rate*100, errorBudget*100)
	}
}

// TestSustainedThroughput reports what this machine sustains against the
// roadmap's 10k events/minute.
//
// It reports rather than asserts, and the distinction is deliberate: latency
// per operation is a property of the code, but throughput is a property of the
// hardware and the database behind it — SQLite is capped at a single connection
// here by design. Asserting a rate would either be so low it proves nothing or
// so high it fails on a busy laptop, and a test that fails for reasons unrelated
// to the change is one people learn to ignore. The number is printed so a
// capacity claim has a measurement behind it.
func TestSustainedThroughput(t *testing.T) {
	h := newHarness(t)

	body := map[string]any{"project_id": h.projID.String(), "definition_key": "slo-approval"}
	r := drive(t, "sustained starts", 16, 600, func(int) sample {
		began := time.Now()
		status := h.call(http.MethodPost, "/api/v1/process/start", h.token, body, nil)
		return sample{elapsed: time.Since(began), status: status}
	})

	perMinute := r.throughputPerMinute()
	backend := "sqlite (single connection)"
	if os.Getenv(testutils.PostgresDSNEnv) != "" {
		backend = "postgres"
	}
	t.Logf("sustained process starts: %.0f/min on %s (roadmap §1 target: 10000 events/min)", perMinute, backend)

	if rate := r.serverErrorRate(); rate > errorBudget {
		t.Errorf("throughput run 5xx rate is %.3f%%, over the %.1f%% budget", rate*100, errorBudget*100)
	}
	if perMinute <= 0 {
		t.Fatal("measured no throughput at all")
	}
}
