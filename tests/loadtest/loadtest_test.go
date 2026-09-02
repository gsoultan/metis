// Package loadtest measures the SLO endpoints against a database with
// production-shaped volume in it.
//
// tests/slo already asserts the same targets, and has to be read for what it
// is: it seeds one organization, one project and one definition, then measures.
// That proves the handler is not pathologically slow. It cannot prove anything
// about behaviour at scale, because an index and a sequential scan are
// indistinguishable over ten rows — a missing index, an N+1, or a query whose
// plan flips at volume all pass there and fail in production.
//
// This is the other half. It bulk-loads hundreds of thousands of rows across
// several tenants and measures the same endpoints, so the failure it exists to
// catch is "this got slower than linear", which is the one that only appears
// once real data has accumulated.
//
// It does not run in the ordinary suite: it needs PostgreSQL, it takes minutes,
// and a test that slow in the default path is a test people start skipping.
//
//	METIS_TEST_POSTGRES_DSN=... METIS_LOADTEST=1 go test ./tests/loadtest/ -v -timeout 30m
//
// Volume is configurable, because the number that matters is yours:
//
//	METIS_LOADTEST_INSTANCES=500000 METIS_LOADTEST_TENANTS=50
package loadtest

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/services"
)

// The targets, from .junie/roadmap.md §1 — the same ones tests/slo asserts.
// Deliberately identical: a target that relaxes as the data grows is not a
// target, it is a description of whatever the system currently does.
const (
	readP95Target = 150 * time.Millisecond
	listP95Target = 150 * time.Millisecond
)

// Defaults sized to be uncomfortable but not slow to seed. A hundred thousand
// instances is a modest year for a real installation, and is already three
// orders of magnitude past what tests/slo measures.
// minMeaningfulBody is the smallest response that could hold a page of rows.
// Below it the endpoint answered with an empty collection, and the timing
// describes an empty answer rather than a query.
const minMeaningfulBody = 512

const (
	defaultInstances = 100_000
	defaultTenants   = 20
	seedBatchSize    = 2_000
)

func TestReadLatencyHoldsAtVolume(t *testing.T) {
	h := newLoadHarness(t)

	t.Logf("seeded %d instances and %d tasks across %d tenants",
		h.instances, h.tasks, h.tenants)

	// Every one of these is a tenant-scoped list — the shape that degrades
	// first, because scoping adds a join and paging adds a sort.
	for _, endpoint := range []struct {
		name string
		path string
	}{
		{"instances (paged)", "/api/v1/instances?project_id=" + h.projectID.String() + "&page=1&page_size=25"},
		{"instances (deep page)", "/api/v1/instances?project_id=" + h.projectID.String() + "&page=200&page_size=25"},
		{"task inbox", "/api/v1/tasks?page=1&page_size=25"},
		{"tasks by assignee", "/api/v1/tasks/assignee/load-0"},
		{"definitions", "/api/v1/definitions?project_id=" + h.projectID.String()},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			r := h.measure(endpoint.path, 200)
			t.Logf("%-24s n=%d p50=%v p95=%v p99=%v max=%v  [status=%d bytes=%d]",
				endpoint.name, r.n, r.p(0.50), r.p(0.95), r.p(0.99), r.p(1.0), h.lastStatus, h.lastBytes)

			// A number measured off an error or an empty page describes
			// nothing. This is the same trap as a test suite that passes by
			// running no tests, and it is worth failing on rather than reading
			// past.
			if h.lastStatus != 200 {
				t.Fatalf("%s answered %d, so the latency above is the cost of refusing the request", endpoint.name, h.lastStatus)
			}
			if h.lastBytes < minMeaningfulBody {
				t.Fatalf("%s returned %d bytes — too small to be a page of results, so this measured an empty answer",
					endpoint.name, h.lastBytes)
			}

			if p95 := r.p(0.95); p95 > listP95Target {
				t.Errorf(""+
					"%s p95 is %v against a %v target, with %d instances and %d tasks in the database.\n"+
					"tests/slo passes on the same endpoint because it measures over almost no rows.\n"+
					"This is what a missing index or an N+1 looks like: fine in development, and only\n"+
					"visible once real data has accumulated.",
					endpoint.name, p95, listP95Target, h.instances, h.tasks)
			}
		})
	}
}

// A deep page is the query most likely to degrade non-linearly: OFFSET makes
// the database walk and discard everything before the page it returns, so page
// 200 costs two hundred pages of work to hand back one.
func TestDeepPagingDoesNotDegradeNonLinearly(t *testing.T) {
	h := newLoadHarness(t)

	first := h.measure("/api/v1/instances?project_id="+h.projectID.String()+"&page=1&page_size=25", 100)
	deep := h.measure("/api/v1/instances?project_id="+h.projectID.String()+"&page=500&page_size=25", 100)

	t.Logf("page 1   p95=%v", first.p(0.95))
	t.Logf("page 500 p95=%v", deep.p(0.95))

	// Ten times is generous: it allows real degradation while still failing on
	// the shape that matters, where the cost grows with the offset rather than
	// with the page.
	if budget := 10 * first.p(0.95); deep.p(0.95) > budget && deep.p(0.95) > listP95Target {
		t.Errorf(""+
			"page 500 costs %v against %v for page 1 — more than ten times, and over the %v target.\n"+
			"OFFSET paging walks and discards every row before the page, so this grows with how far in\n"+
			"the user has scrolled. Keyset paging is the fix if this is a real access pattern.",
			deep.p(0.95), first.p(0.95), listP95Target)
	}
}

// ---------------------------------------------------------------- the harness

type loadHarness struct {
	*sloHarness
	service   services.ServiceFacade
	instances int
	tasks     int
	tenants   int
	projectID uuid.UUID
}

func newLoadHarness(t *testing.T) *loadHarness {
	t.Helper()
	requireOptIn(t)

	base, svc := newSLOHarnessWithService(t)
	h := &loadHarness{
		sloHarness: base,
		service:    svc,
		tenants:    intFromEnv("METIS_LOADTEST_TENANTS", defaultTenants),
	}
	h.projectID = base.projID
	h.seedVolume(t, intFromEnv("METIS_LOADTEST_INSTANCES", defaultInstances))
	return h
}

// requireOptIn keeps this out of the ordinary suite.
//
// Skipping rather than failing, but loudly: a suite that silently does nothing
// is the failure this repository has had twice already, so the skip says
// exactly what it needed.
func requireOptIn(t *testing.T) {
	t.Helper()
	if os.Getenv("METIS_LOADTEST") == "" {
		t.Skip("set METIS_LOADTEST=1 to run the load test; it needs PostgreSQL and takes minutes")
	}
	if os.Getenv("METIS_TEST_POSTGRES_DSN") == "" {
		t.Fatal("METIS_LOADTEST is set but METIS_TEST_POSTGRES_DSN is not. " +
			"SQLite would measure a different database than production runs, which is worse than not measuring")
	}
}

func intFromEnv(name string, fallback int) int {
	if raw := os.Getenv(name); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

// measure issues n requests and returns their latencies.
//
// Sequential on purpose. This is measuring the cost of one query against a
// large table, not concurrency — mixing the two produces a number that moves
// when either changes and explains neither.
func (h *loadHarness) measure(path string, n int) report {
	h.t.Helper()

	latencies := make([]time.Duration, 0, n)
	for range n {
		start := time.Now()
		status := h.get(path)
		elapsed := time.Since(start)
		if status >= 500 {
			h.t.Fatalf("GET %s returned %d", path, status)
		}
		latencies = append(latencies, elapsed)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	return report{latencies: latencies, n: n}
}

type report struct {
	latencies []time.Duration
	n         int
}

func (r report) p(q float64) time.Duration {
	if len(r.latencies) == 0 {
		return 0
	}
	i := int(float64(len(r.latencies)-1) * q)
	return r.latencies[i].Round(time.Microsecond)
}

// seedVolume writes rows straight through GORM rather than over HTTP.
//
// Seeding a hundred thousand instances through the API would take longer than
// the measurement and would be measuring the write path, which is not what this
// test is about. The rows are the same rows either way.
func (h *loadHarness) seedVolume(t *testing.T, instances int) {
	t.Helper()
	start := time.Now()

	tenantProjects := h.seedTenants(t, h.tenants)

	perTenant := instances / len(tenantProjects)
	total, totalTasks := 0, 0
	for _, projectID := range tenantProjects {
		created, tasks := h.seedInstances(t, projectID, perTenant)
		total += created
		totalTasks += tasks
	}

	h.instances, h.tasks = total, totalTasks
	t.Logf("seeding took %v", time.Since(start).Round(time.Second))
}

func (h *loadHarness) seedInstances(t *testing.T, projectID uuid.UUID, n int) (instances, tasks int) {
	t.Helper()

	for offset := 0; offset < n; offset += seedBatchSize {
		size := min(seedBatchSize, n-offset)
		instanceRows := make([]instanceRow, 0, size)
		taskRows := make([]taskRow, 0, size)

		for i := range size {
			id := uuid.New()
			// Spread over the past year so that anything ordering by time has
			// a realistic distribution rather than one instant.
			created := time.Now().Add(-time.Duration(offset+i) * time.Minute)

			instanceRows = append(instanceRows, instanceRow{
				ID: id, ProjectID: projectID, DefinitionID: h.definitionID,
				Status: "running", CreatedAt: created, UpdatedAt: created,
			})
			// Most instances of a one-human-step process are sitting on that
			// step, which is what makes the inbox the table that grows.
			taskRows = append(taskRows, taskRow{
				ID: uuid.New(), ProjectID: projectID, InstanceID: id,
				NodeID: "approve", Name: "Approve", Type: "userTask", Status: "unclaimed",
				Assignee:  fmt.Sprintf("load-%d", (offset+i)%h.tenants),
				CreatedAt: created, UpdatedAt: created,
			})
		}

		h.insert(t, "process_instances", instanceRows)
		h.insert(t, "tasks", taskRows)
		instances += len(instanceRows)
		tasks += len(taskRows)
	}
	return instances, tasks
}

func (h *loadHarness) insert(t *testing.T, table string, rows any) {
	t.Helper()
	if err := h.db.Table(table).Create(rows).Error; err != nil {
		t.Fatalf("bulk insert into %s: %v", table, err)
	}
}

// The rows are declared here rather than reusing the models because the models
// carry associations GORM would try to insert alongside them, which turns one
// batch into thousands of statements.
type instanceRow struct {
	ID           uuid.UUID `gorm:"column:id"`
	ProjectID    uuid.UUID `gorm:"column:project_id"`
	DefinitionID uuid.UUID `gorm:"column:definition_id"`
	Status       string    `gorm:"column:status"`
	CreatedAt    time.Time `gorm:"column:created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at"`
}

type taskRow struct {
	ID         uuid.UUID `gorm:"column:id"`
	ProjectID  uuid.UUID `gorm:"column:project_id"`
	InstanceID uuid.UUID `gorm:"column:instance_id"`
	NodeID     string    `gorm:"column:node_id"`
	Name       string    `gorm:"column:name"`
	Type       string    `gorm:"column:type"`
	Status     string    `gorm:"column:status"`
	Assignee   string    `gorm:"column:assignee"`
	CreatedAt  time.Time `gorm:"column:created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at"`
}
