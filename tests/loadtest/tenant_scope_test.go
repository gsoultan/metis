package loadtest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/pg"
)

// What tenant scoping costs an organization with ten thousand projects.
//
// Every scoped repository call works out what the caller may see from the ids
// of every project in the caller's organization, and scopes its query with
// project_id = ANY(those ids). A request makes several such calls — the
// dashboard's statistics make six — and each used to read the list again, so
// an organization paid for the size of its project list once per call. A
// request reads it once now (tenantscope.Request); this holds it to that, and
// to the read target, at ten thousand projects. Every other organization in
// this package owns one project.
//
// Everything but the project count is held equal. Both organizations have the
// same instances and tasks over the same number of busy projects; the large
// one also owns thousands of projects with nothing in them, which is what an
// organization with many projects mostly looks like. So the difference between
// the two columns is the project list and nothing else. The two are sampled in
// turn, request by request, so drift on the machine lands on both.
//
//	METIS_TEST_POSTGRES_DSN=... METIS_LOADTEST=1 go test ./tests/loadtest/ -run TenantScope -v -timeout 30m
//
// Sized with METIS_LOADTEST_SCOPE_PROJECTS (the large organization's projects)
// and METIS_LOADTEST_SCOPE_INSTANCES (instances per busy project).

const (
	defaultScopeProjects  = 10_000
	defaultScopeInstances = 5_000
	// scopeBusyProjects is how many of an organization's projects hold its
	// work, in both organizations.
	scopeBusyProjects = 4
	scopeSampleCount  = 200
	scopeWarmup       = 20
	scopePassword     = "scope-load-test-password"
	// statisticsPath is the dashboard's statistics call: a Connect RPC, sent as
	// JSON the way the UI sends it.
	statisticsPath = "/api/v1/process.StatsService/GetProcessStatistics"
)

func TestTenantScopeAtScale(t *testing.T) {
	h := newScopeHarness(t)

	var results []scopeResult
	for _, endpoint := range h.endpoints() {
		t.Run(endpoint.name, func(t *testing.T) {
			small, large := h.measurePair(t, endpoint)
			results = append(results, scopeResult{endpoint: endpoint.name, small: small, large: large})
			t.Logf("%-22s small p50=%-9v p95=%-9v reads/request=%-3d %6d KB/request  [%d bytes]",
				endpoint.name, small.p(0.50), small.p(0.95), small.readsPerRequest(), small.kilobytesPerRequest(), small.bytes)
			t.Logf("%-22s large p50=%-9v p95=%-9v reads/request=%-3d %6d KB/request  [%d bytes]  (p95 %+v)",
				endpoint.name, large.p(0.50), large.p(0.95), large.readsPerRequest(), large.kilobytesPerRequest(), large.bytes,
				large.p(0.95)-small.p(0.95))
			assertScopeCost(t, endpoint.name, h.small, small)
			assertScopeCost(t, endpoint.name, h.large, large)
		})
	}

	t.Logf("%d projects against %d, %d instances and as many tasks in each organization",
		h.large.projects, h.small.projects, h.small.instances)
	for _, r := range results {
		t.Logf("| %s | %v | %v | %d | %d | %d | %d |", r.endpoint,
			r.small.p(0.95), r.large.p(0.95), r.small.readsPerRequest(), r.large.readsPerRequest(),
			r.small.kilobytesPerRequest(), r.large.kilobytesPerRequest())
	}
}

// assertScopeCost holds what one organization paid for one read to what
// scoping may cost: its project list read once per request, however many scoped
// calls the request makes, and the read target met whatever the organization's
// size.
//
// The difference between the two organizations is logged, not asserted. What
// is left of it is the project list inside the query, whose cost depends on the
// plan PostgreSQL picks for it — docs/performance.md has the measurement.
func assertScopeCost(t *testing.T, endpoint string, o scopeOrganization, s scopeSamples) {
	t.Helper()
	if slices.ContainsFunc(s.reads, func(reads int64) bool { return reads != 1 }) {
		t.Errorf("%s as the %s organization read its project list between %d and %d times a request, want once: "+
			"every scoped call in a request reuses one read", endpoint, o.label, slices.Min(s.reads), slices.Max(s.reads))
	}
	if p95 := s.p(0.95); p95 > readP95Target {
		t.Errorf("%s as the %s organization, with %d projects: p95 %v against the %v read target",
			endpoint, o.label, o.projects, p95, readP95Target)
	}
}

// ---------------------------------------------------------------- the harness

type scopeHarness struct {
	*loadHarness
	small, large scopeOrganization
}

// scopeOrganization is one side of the comparison, and what its reads name.
type scopeOrganization struct {
	label     string
	token     string
	projects  int
	instances int
	// busy is the project the per-project reads name; instance and task are
	// read by id.
	busy     uuid.UUID
	instance uuid.UUID
	task     uuid.UUID
}

type scopeEndpoint struct {
	name   string
	method string
	// path and body are per organization: most reads name a project, an
	// instance or a task of the caller's own.
	path func(scopeOrganization) string
	body func(scopeOrganization) []byte
	// minBytes is the smallest answer that could hold what was asked for.
	// Below it the endpoint answered with nothing, and the timing describes an
	// empty answer rather than a query.
	minBytes int64
}

func newScopeHarness(t *testing.T) *scopeHarness {
	t.Helper()
	requireOptIn(t)

	base, svc := newSLOHarnessWithService(t)
	h := &scopeHarness{loadHarness: &loadHarness{sloHarness: base, service: svc, tenants: defaultTenants}}

	start := time.Now()
	instances := intFromEnv("METIS_LOADTEST_SCOPE_INSTANCES", defaultScopeInstances)
	h.small = h.seedOrganization(t, "small", scopeBusyProjects, instances)
	h.large = h.seedOrganization(t, "large", intFromEnv("METIS_LOADTEST_SCOPE_PROJECTS", defaultScopeProjects), instances)

	// Planner statistics, as a database that has been running would have
	// them. Straight after a bulk load autovacuum has not caught up, and a
	// plan chosen on no statistics measures the load rather than the query.
	if err := h.db.Exec("ANALYZE projects, process_definitions, process_instances, tasks").Error; err != nil {
		t.Fatalf("analyze: %v", err)
	}
	t.Logf("seeding took %v", time.Since(start).Round(time.Second))
	return h
}

// seedOrganization creates an organization with projects projects, work in
// scopeBusyProjects of them, and an administrator signed in to read it.
func (h *scopeHarness) seedOrganization(t *testing.T, label string, projects, instancesPerProject int) scopeOrganization {
	t.Helper()
	ctx := t.Context()

	org, err := h.service.CreateOrganization(ctx, "Scope "+label, "")
	if err != nil {
		t.Fatalf("create the %s organization: %v", label, err)
	}
	tenantCtx := entities.WithTenantContext(ctx, entities.TenantContext{TenantID: org.ID.String()})

	seeded := scopeOrganization{label: label, projects: max(projects, scopeBusyProjects)}
	for i := range scopeBusyProjects {
		project, err := h.service.CreateProject(tenantCtx, org.ID, fmt.Sprintf("Busy %d", i), "")
		if err != nil {
			t.Fatalf("create a busy project in %s: %v", label, err)
		}
		definitionID, err := h.service.CreateDefinition(tenantCtx, approvalDefinition(project.ID))
		if err != nil {
			t.Fatalf("deploy into a busy project in %s: %v", label, err)
		}
		created, _ := h.seedInstances(t, project.ID, definitionID, instancesPerProject)
		seeded.instances += created
		if i == 0 {
			seeded.busy = project.ID
		}
	}

	// The rest hold nothing. Written in one statement: ten thousand projects
	// through the service would be ten thousand requests' worth of seeding.
	if idle := projects - scopeBusyProjects; idle > 0 {
		if err := h.db.Exec(`
			INSERT INTO projects (id, created_at, updated_at, organization_id, name)
			SELECT gen_random_uuid(), now(), now(), ?, format('Idle %s', lpad(n::text, 6, '0'))
			  FROM generate_series(1, ?) AS n`, org.ID, idle).Error; err != nil {
			t.Fatalf("seed %d idle projects in %s: %v", idle, label, err)
		}
	}

	username := "scope-" + label
	if err := h.service.CreateUser(tenantCtx, entities.User{
		Username:      username,
		Roles:         []string{entities.RoleAdmin},
		Organizations: []*entities.Organization{{ID: org.ID}},
	}, scopePassword); err != nil {
		t.Fatalf("create the %s administrator: %v", label, err)
	}
	seeded.token = h.loginAs(username, scopePassword)
	seeded.instance = h.firstID(t, "process_instances", seeded.busy)
	seeded.task = h.firstID(t, "tasks", seeded.busy)
	return seeded
}

func (h *scopeHarness) firstID(t *testing.T, table string, projectID uuid.UUID) uuid.UUID {
	t.Helper()
	var id string
	if err := h.db.Raw("SELECT id::text FROM "+table+" WHERE project_id = ? ORDER BY created_at DESC LIMIT 1", projectID).
		Scan(&id).Error; err != nil {
		t.Fatalf("find a row in %s: %v", table, err)
	}
	parsed, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("find a row in %s: %q is not an id: %v", table, id, err)
	}
	return parsed
}

// endpoints are the reads a person opening the product makes: the inbox, the
// instance list and one instance, the models, and the dashboard.
func (h *scopeHarness) endpoints() []scopeEndpoint {
	get := func(name string, minBytes int64, path func(scopeOrganization) string) scopeEndpoint {
		return scopeEndpoint{name: name, method: http.MethodGet, path: path, minBytes: minBytes}
	}
	return []scopeEndpoint{
		get("task inbox", minMeaningfulBody, func(scopeOrganization) string { return "/api/v1/tasks?page=1&page_size=25" }),
		get("tasks by assignee", minMeaningfulBody, func(scopeOrganization) string { return "/api/v1/tasks/assignee/load-0" }),
		get("task by id", 128, func(o scopeOrganization) string { return "/api/v1/tasks/" + o.task.String() }),
		get("instances (paged)", minMeaningfulBody, func(o scopeOrganization) string {
			return "/api/v1/instances?project_id=" + o.busy.String() + "&page=1&page_size=25"
		}),
		get("instance by id", 128, func(o scopeOrganization) string { return "/api/v1/instances/" + o.instance.String() }),
		get("definitions", 128, func(o scopeOrganization) string { return "/api/v1/definitions?project_id=" + o.busy.String() }),
		get("waiting by step", 64, func(o scopeOrganization) string { return "/api/v1/projects/" + o.busy.String() + "/waiting" }),
		// Connect leaves out a count of zero, so an answer with nothing counted
		// is "{}" and one with counts in it is already a few dozen bytes.
		{
			name: "dashboard statistics", method: http.MethodPost, minBytes: 32,
			path: func(scopeOrganization) string { return statisticsPath },
			body: func(o scopeOrganization) []byte {
				body, _ := json.Marshal(map[string]string{"projectId": o.busy.String()})
				return body
			},
		},
	}
}

// measurePair samples one endpoint as each organization in turn.
func (h *scopeHarness) measurePair(t *testing.T, e scopeEndpoint) (small, large scopeSamples) {
	t.Helper()
	for range scopeWarmup {
		h.sample(t, e, h.small)
		h.sample(t, e, h.large)
	}
	for range scopeSampleCount {
		small.add(h.sample(t, e, h.small))
		large.add(h.sample(t, e, h.large))
	}
	return small, large
}

// sample is one request, with what it cost: how long it took, how many times
// it read an organization's project list, and what it allocated. The server
// runs in this process, so the allocation is the handler's and the client's
// together — the same client either way, so a difference is the handler's.
func (h *scopeHarness) sample(t *testing.T, e scopeEndpoint, o scopeOrganization) scopeSample {
	t.Helper()
	var body []byte
	if e.body != nil {
		body = e.body(o)
	}
	path := e.path(o)

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	reads := pg.ScopeReads()
	start := time.Now()
	status, n := h.send(e.method, path, o.token, body)
	elapsed := time.Since(start)
	reads = pg.ScopeReads() - reads
	runtime.ReadMemStats(&after)

	// A number measured off an error or an empty answer describes nothing.
	if status != http.StatusOK {
		t.Fatalf("%s as the %s organization answered %d, so its latency is the cost of refusing it", e.name, o.label, status)
	}
	if n < e.minBytes {
		t.Fatalf("%s as the %s organization returned %d bytes, too few to be what was asked for", e.name, o.label, n)
	}
	return scopeSample{latency: elapsed, reads: reads, allocated: after.TotalAlloc - before.TotalAlloc, bytes: n}
}

type scopeSample struct {
	latency   time.Duration
	reads     int64
	allocated uint64
	bytes     int64
}

type scopeSamples struct {
	latencies []time.Duration
	reads     []int64
	allocated uint64
	// bytes is the size of the last answer, so the log says what was timed.
	bytes int64
}

func (s *scopeSamples) add(one scopeSample) {
	s.latencies = append(s.latencies, one.latency)
	s.reads = append(s.reads, one.reads)
	s.allocated += one.allocated
	s.bytes = one.bytes
}

func (s scopeSamples) p(q float64) time.Duration {
	sorted := slices.Sorted(slices.Values(s.latencies))
	return report{latencies: sorted, n: len(sorted)}.p(q)
}

// readsPerRequest is the most any one request read. Every request of one
// endpoint takes the same path, so this is also the typical count.
func (s scopeSamples) readsPerRequest() int64 {
	if len(s.reads) == 0 {
		return 0
	}
	return slices.Max(s.reads)
}

func (s scopeSamples) kilobytesPerRequest() uint64 {
	if len(s.latencies) == 0 {
		return 0
	}
	return s.allocated / uint64(len(s.latencies)) / 1024
}

type scopeResult struct {
	endpoint     string
	small, large scopeSamples
}
