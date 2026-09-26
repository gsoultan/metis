package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/services"
	"github.com/gsoultan/metis/server/repositories/models"
	"gorm.io/gorm"
)

// The engine's backlog gauges read the main database only. A job worker that
// stopped claiming in an environment — or an environment that could not be
// opened at all, whose jobs nobody will ever claim — looked exactly like an
// installation with nothing to do, which is the one thing the gauges exist to
// tell apart from trouble.
func TestTheMetricsEndpointReportsEachEnvironmentsBacklog(t *testing.T) {
	fastEnvironmentChecks(t)
	h := newEnvironmentHarness(t)
	// Nothing claims the job below: the stalled worker the gauges are for.
	h.app.svc = withoutWorkers{h.app.svc}
	stagingDB := scratchDatabase(t)
	staging := h.environment("staging", stagingDB)
	h.environment("broken", unusedDatabaseName())
	metrics := h.runServers()

	within(t, startLimit, "the staging environment was never served", func() bool { return !refuses(t, staging.Port) })
	stalled(t, behindTheServer(t, stagingDB))

	var scrape map[string][]series
	within(t, startLimit, "no scrape counted the job due in staging", func() bool {
		scrape = scrapeEngine(t, metrics)
		due, ok := only(scrape["metis_jobs_due"], staging.Name)
		return ok && due.value == 1
	})

	// The main database's series are the ones there have always been: to
	// Prometheus an empty label is no label, so their identity is unchanged.
	for _, name := range []string{"metis_engine_state_up", "metis_jobs_due", "metis_incidents_open"} {
		main, ok := only(scrape[name], "")
		if !ok || len(main.labels) != 0 {
			t.Fatalf("%s for the main database is %+v; it must carry no environment label", name, scrape[name])
		}
	}
	if up, _ := only(scrape["metis_engine_state_up"], ""); up.value != 1 {
		t.Fatalf("the main database's backlog was not readable: up = %v", up.value)
	}

	for name, want := range map[string]float64{
		"metis_engine_state_up": 1, "metis_jobs_due": 1, "metis_incidents_open": 1, "metis_jobs_lease_expired": 0,
	} {
		got, ok := only(scrape[name], staging.Name)
		if !ok || got.value != want {
			t.Fatalf("%s for staging is %+v, want %v", name, scrape[name], want)
		}
		if got.labels["environment"] != uuid.UUID(staging.ID).String() {
			t.Fatalf("staging's %s is labelled %v; the label must be its id, which a rename does not change", name, got.labels)
		}
	}
	if oldest, _ := only(scrape["metis_jobs_oldest_due_age_seconds"], staging.Name); oldest.value < 60 {
		t.Fatalf("the job in staging has been due for over an hour, and the gauge says %vs", oldest.value)
	}

	// One environment that cannot be read is down on its own: the others keep
	// their series.
	if up, ok := only(scrape["metis_engine_state_up"], "broken"); !ok || up.value != 0 {
		t.Fatalf("the environment whose database does not exist is not reported down: %+v", scrape["metis_engine_state_up"])
	}
	if _, ok := only(scrape["metis_jobs_due"], "broken"); ok {
		t.Fatal("an unreadable environment reported a backlog; it must be absent, not zero")
	}
}

// withoutWorkers is the service with its job workers never started.
type withoutWorkers struct{ services.ServiceFacade }

func (withoutWorkers) StartWorkers(context.Context) {}

// stalled leaves in a database what a worker that stopped claiming leaves: a
// job an hour overdue, and an incident nobody has resolved.
func stalled(t *testing.T, database *gorm.DB) {
	t.Helper()
	job := models.JobModel{
		InstanceID: models.UUID(uuid.New()), DefinitionID: models.UUID(uuid.New()),
		NodeID: "remind", Type: models.JobTimer, Status: models.JobPending,
		Payload: map[string]any{}, NextRunAt: time.Now().Add(-time.Hour),
	}
	if err := database.Create(&job).Error; err != nil {
		t.Fatalf("leave a due job: %v", err)
	}
	incident := models.IncidentModel{
		JobID: job.ID, InstanceID: job.InstanceID, DefinitionID: job.DefinitionID,
		NodeID: "remind", Error: "the partner did not answer", Status: models.IncidentOpen,
	}
	if err := database.Create(&incident).Error; err != nil {
		t.Fatalf("leave an open incident: %v", err)
	}
}

// runServers runs the server's listeners as Run does, on free loopback
// addresses, and returns the metrics address.
func (h *environmentHarness) runServers() string {
	h.t.Helper()
	httpAddress, metricsAddress := fmt.Sprintf("127.0.0.1:%d", freePort(h.t)), fmt.Sprintf("127.0.0.1:%d", freePort(h.t))
	h.t.Setenv(envHTTPAddress, httpAddress)
	h.t.Setenv(envMetricsEnabled, "true")
	h.t.Setenv(envMetricsAddress, metricsAddress)
	h.t.Setenv(envGRPCAddress, "")
	h.t.Setenv(envPprofEnabled, "false")
	h.t.Setenv("METIS_SHUTDOWN_DRAIN", "0s")

	ctx, cancel := context.WithCancel(h.t.Context())
	stopped := make(chan error, 1)
	go func() { stopped <- h.app.runServers(ctx) }()
	h.t.Cleanup(func() {
		cancel()
		if err := <-stopped; err != nil {
			h.t.Errorf("the server did not stop cleanly: %v", err)
		}
		h.app.environments.work.Wait()
		h.closeEnvironments()
	})
	return metricsAddress
}

// series is one line of a scrape.
type series struct {
	labels map[string]string
	value  float64
}

var (
	sampleLine = regexp.MustCompile(`^(\w+)(?:\{(.*)\})? (\S+)$`)
	labelPair  = regexp.MustCompile(`(\w+)="((?:[^"\\]|\\.)*)"`)
)

// scrapeEngine reads the engine's series off the metrics endpoint. Labels with
// an empty value are dropped, as Prometheus drops them when it stores a scrape.
func scrapeEngine(t *testing.T, address string) map[string][]series {
	t.Helper()
	client := http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+address+"/metrics", nil)
	if err != nil {
		t.Fatalf("build the scrape: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	scrape := map[string][]series{}
	lines := bufio.NewScanner(strings.NewReader(string(body)))
	for lines.Scan() {
		match := sampleLine.FindStringSubmatch(lines.Text())
		if match == nil || !engineSeries[match[1]] {
			continue
		}
		value, err := strconv.ParseFloat(match[3], 64)
		if err != nil {
			t.Fatalf("read %q: %v", lines.Text(), err)
		}
		labels := map[string]string{}
		for _, pair := range labelPair.FindAllStringSubmatch(match[2], -1) {
			if pair[2] != "" {
				labels[pair[1]] = pair[2]
			}
		}
		scrape[match[1]] = append(scrape[match[1]], series{labels: labels, value: value})
	}
	return scrape
}

var engineSeries = map[string]bool{
	"metis_engine_state_up": true, "metis_jobs_due": true, "metis_jobs_oldest_due_age_seconds": true,
	"metis_jobs_lease_expired": true, "metis_incidents_open": true,
}

// only returns the one series for an environment, by name; "" is the main
// database.
func only(all []series, environment string) (series, bool) {
	var found []series
	for _, s := range all {
		if s.labels["environment_name"] == environment {
			found = append(found, s)
		}
	}
	if len(found) != 1 {
		return series{}, false
	}
	return found[0], true
}
