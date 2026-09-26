package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// mainOnly is an installation with no environments.
func mainOnly(probe EngineProbe) EngineSources {
	return func() []EngineSource { return []EngineSource{{Probe: probe}} }
}

func TestTheEngineCollectorReportsTheBacklog(t *testing.T) {
	collector := NewEngineCollector(mainOnly(func(context.Context) (EngineState, error) {
		return EngineState{DueJobs: 12, OldestDueAge: 90 * time.Second, LeaseExpiredJobs: 2, OpenIncidents: 5}, nil
	}))
	for name, want := range map[string]float64{
		"metis_engine_state_up":             1,
		"metis_jobs_due":                    12,
		"metis_jobs_oldest_due_age_seconds": 90,
		"metis_jobs_lease_expired":          2,
		"metis_incidents_open":              5,
	} {
		if got := gathered(t, collector, name); got != want {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
}

// A database that cannot answer must not look like an empty queue. The backlog
// series go absent and `up` says why, so an alert on the backlog cannot be
// quieted by the very outage it should be reporting.
func TestAnUnreadableBacklogIsNotReportedAsAnEmptyOne(t *testing.T) {
	collector := NewEngineCollector(mainOnly(func(context.Context) (EngineState, error) {
		return EngineState{}, errors.New("connection refused")
	}))
	if got := gathered(t, collector, "metis_engine_state_up"); got != 0 {
		t.Fatalf("up = %v, want 0", got)
	}
	if n := testutil.CollectAndCount(collector, "metis_jobs_due", "metis_jobs_oldest_due_age_seconds"); n != 0 {
		t.Fatalf("%d backlog series reported while the backlog could not be read", n)
	}
}

// The probe gets a deadline: a scrape must not wait on a database that has
// stopped answering.
func TestTheEngineProbeHasADeadline(t *testing.T) {
	var deadline time.Time
	collector := NewEngineCollector(mainOnly(func(ctx context.Context) (EngineState, error) {
		deadline, _ = ctx.Deadline()
		return EngineState{}, nil
	}))
	gathered(t, collector, "metis_engine_state_up")
	if deadline.IsZero() || time.Until(deadline) > engineProbeBudget {
		t.Fatalf("the probe ran with deadline %v", deadline)
	}
}

// Two environments beside the main database.
const (
	stagingID    = "0199a1b2-0000-7000-8000-000000000001"
	productionID = "0199a1b2-0000-7000-8000-000000000002"
)

func threeDatabases(staging, production EngineProbe) EngineSources {
	return func() []EngineSource {
		return []EngineSource{
			{Probe: answering(EngineState{DueJobs: 1, OpenIncidents: 1})},
			{Environment: stagingID, EnvironmentName: "staging", Probe: staging},
			{Environment: productionID, EnvironmentName: "production", Probe: production},
		}
	}
}

func answering(state EngineState) EngineProbe {
	return func(context.Context) (EngineState, error) { return state, nil }
}

// Each environment's backlog is its own set of series, labelled with the
// environment's id and name, and the main database's set is exactly the one
// there has always been: no environment label, because to Prometheus an empty
// label is no label.
func TestEachEnvironmentsBacklogIsItsOwnSeries(t *testing.T) {
	collector := NewEngineCollector(threeDatabases(
		answering(EngineState{DueJobs: 7, OldestDueAge: time.Hour, OpenIncidents: 3}),
		answering(EngineState{LeaseExpiredJobs: 2}),
	))
	due := byEnvironment(gatheredSeries(t, collector, "metis_jobs_due"))
	if len(due) != 3 {
		t.Fatalf("metis_jobs_due has %d series, want one per database: %v", len(due), due)
	}
	for environment, want := range map[string]float64{"": 1, stagingID: 7, productionID: 0} {
		if got, ok := due[environment]; !ok || got != want {
			t.Errorf("metis_jobs_due{environment=%q} = %v (present %v), want %v", environment, got, ok, want)
		}
	}
	if got := byEnvironment(gatheredSeries(t, collector, "metis_jobs_oldest_due_age_seconds"))[stagingID]; got != time.Hour.Seconds() {
		t.Errorf("staging's oldest due job waited %vs, want an hour", got)
	}
	if got := byEnvironment(gatheredSeries(t, collector, "metis_jobs_lease_expired"))[productionID]; got != 2 {
		t.Errorf("production's expired leases = %v, want 2", got)
	}

	names := map[string]string{"": "", stagingID: "staging", productionID: "production"}
	for _, s := range gatheredSeries(t, collector, "metis_engine_state_up") {
		if want := names[s.labels["environment"]]; s.labels["environment_name"] != want {
			t.Errorf("the series for environment %q is named %q, want %q", s.labels["environment"], s.labels["environment_name"], want)
		}
		if len(s.labels) != 2 {
			t.Errorf("an engine series carries labels %v; environment and environment_name are the only ones", s.labels)
		}
	}
}

// One environment whose database cannot be read is reported down on its own.
// Its backlog is absent rather than zero, and every other database keeps its
// series.
func TestAnUnreadableEnvironmentIsDownOnItsOwn(t *testing.T) {
	collector := NewEngineCollector(threeDatabases(
		func(context.Context) (EngineState, error) { return EngineState{}, errors.New("connection refused") },
		answering(EngineState{DueJobs: 4}),
	))
	up := byEnvironment(gatheredSeries(t, collector, "metis_engine_state_up"))
	for environment, want := range map[string]float64{"": 1, stagingID: 0, productionID: 1} {
		if got := up[environment]; got != want {
			t.Errorf("metis_engine_state_up{environment=%q} = %v, want %v", environment, got, want)
		}
	}
	due := byEnvironment(gatheredSeries(t, collector, "metis_jobs_due"))
	if _, ok := due[stagingID]; ok {
		t.Error("the unreadable environment reported a backlog; it must be absent, not zero")
	}
	if due[productionID] != 4 || due[""] != 1 {
		t.Errorf("the readable databases lost their backlog: %v", due)
	}
}

// An environment this replica has no connection to — one that could not be
// opened — has no probe, and is reported down.
func TestAnEnvironmentWithNoConnectionIsDown(t *testing.T) {
	collector := NewEngineCollector(threeDatabases(nil, answering(EngineState{})))
	up := byEnvironment(gatheredSeries(t, collector, "metis_engine_state_up"))
	if got, ok := up[stagingID]; !ok || got != 0 {
		t.Fatalf("an environment with no connection is up = %v (present %v), want 0", got, ok)
	}
}

// A database that does not answer costs its own series, not the scrape: the
// others are read at the same time, under the same budget, and reported.
func TestADatabaseThatDoesNotAnswerDoesNotHoldUpTheScrape(t *testing.T) {
	hung := make(chan struct{})
	t.Cleanup(func() { close(hung) })
	collector := NewEngineCollector(threeDatabases(
		// Ignores its deadline altogether, which is the worst a probe can do.
		func(context.Context) (EngineState, error) { <-hung; return EngineState{}, nil },
		answering(EngineState{DueJobs: 4}),
	))
	start := time.Now()
	up := byEnvironment(gatheredSeries(t, collector, "metis_engine_state_up"))
	if took := time.Since(start); took > 2*engineProbeBudget {
		t.Fatalf("the scrape took %v with one database not answering; the budget is %v", took, engineProbeBudget)
	}
	for environment, want := range map[string]float64{"": 1, stagingID: 0, productionID: 1} {
		if got := up[environment]; got != want {
			t.Errorf("metis_engine_state_up{environment=%q} = %v, want %v", environment, got, want)
		}
	}
}

// gaugeSeries is one gathered gauge.
type gaugeSeries struct {
	labels map[string]string
	value  float64
}

// gatheredSeries gathers one metric's series through a registry, as a scrape
// would.
func gatheredSeries(t *testing.T, collector prometheus.Collector, name string) []gaugeSeries {
	t.Helper()
	registry := prometheus.NewRegistry()
	registry.MustRegister(collector)
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	var all []gaugeSeries
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, pair := range metric.GetLabel() {
				labels[pair.GetName()] = pair.GetValue()
			}
			all = append(all, gaugeSeries{labels: labels, value: metric.GetGauge().GetValue()})
		}
	}
	return all
}

// byEnvironment keys series by their environment label, "" for the main
// database.
func byEnvironment(all []gaugeSeries) map[string]float64 {
	values := map[string]float64{}
	for _, s := range all {
		values[s.labels["environment"]] = s.value
	}
	return values
}
