package metrics

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"
)

// EngineState is how far behind the engine is, read from the database.
type EngineState struct {
	// DueJobs is how many jobs are waiting whose time has come.
	DueJobs int64
	// OldestDueAge is how long the longest-waiting of them has waited; zero
	// when none is.
	OldestDueAge time.Duration
	// LeaseExpiredJobs is how many a worker claimed and stopped renewing.
	LeaseExpiredJobs int64
	// OpenIncidents is how many incidents nobody has resolved.
	OpenIncidents int64
}

// EngineProbe reads one database's backlog, once per scrape.
type EngineProbe func(ctx context.Context) (EngineState, error)

// EngineSource is one database whose backlog is reported: the main one, or an
// environment's.
type EngineSource struct {
	// Environment is the id of the environment the database belongs to, and
	// EnvironmentName what people call it. Both are empty for the main
	// database, so its series keep the identity they have always had: to
	// Prometheus an empty label is no label at all.
	//
	// The id is the label that identifies: unique, unchanged by a rename, and
	// one value per environment. The name rides along so a graph or an alert
	// can be read without looking an id up; renaming an environment therefore
	// starts its series afresh under the new name, which is rare, and which an
	// aggregation by environment does not notice.
	Environment, EnvironmentName string
	// Probe reads the backlog. Nil when this replica holds no connection to
	// the database — an environment that could not be opened, which is
	// reported where that failed — and the source is then reported down
	// without a second log line saying so every scrape.
	Probe EngineProbe
}

// EngineSources lists, at each scrape, the databases to report.
type EngineSources func() []EngineSource

// engineProbeBudget bounds one scrape's reads. Well inside a scrape timeout,
// so a slow database shows up as `metis_engine_state_up 0` rather than as a
// scrape that failed and took every other series with it.
const engineProbeBudget = 2 * time.Second

// engineLabels are the labels on every engine series.
var engineLabels = []string{"environment", "environment_name"}

// engineCollector reports the engine's backlog at scrape time, one set of
// series per database.
//
// The HTTP metrics say nothing about the part of the engine nobody is waiting
// on: a job worker that stopped claiming looks exactly like a system with
// nothing to do. These are the series that tell the two apart. They are read
// at scrape time, not counted as work happens, because the question is "how
// far behind is the queue now", which only the database can answer across
// replicas — and each environment's queue is in that environment's database.
type engineCollector struct {
	sources EngineSources

	up, due, oldest, leaseExpired, incidents *prometheus.Desc
}

// NewEngineCollector reports the backlog of every database sources lists.
func NewEngineCollector(sources EngineSources) prometheus.Collector {
	return &engineCollector{
		sources: sources,
		up: prometheus.NewDesc("metis_engine_state_up",
			"1 when a database's backlog could be read at this scrape. The series below are absent for it while it is 0. "+
				"One per database: the main one, with no environment label, and each environment's.", engineLabels, nil),
		due: prometheus.NewDesc("metis_jobs_due",
			"Jobs whose time has come that no worker has claimed.", engineLabels, nil),
		oldest: prometheus.NewDesc("metis_jobs_oldest_due_age_seconds",
			"How long the longest-waiting due job has waited. Grows without bound when no worker is claiming.", engineLabels, nil),
		leaseExpired: prometheus.NewDesc("metis_jobs_lease_expired",
			"Jobs a worker claimed and stopped renewing. The next poll reclaims them, so this should not stay above zero.", engineLabels, nil),
		incidents: prometheus.NewDesc("metis_incidents_open",
			"Incidents nobody has resolved.", engineLabels, nil),
	}
}

func (c *engineCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.up
	ch <- c.due
	ch <- c.oldest
	ch <- c.leaseExpired
	ch <- c.incidents
}

func (c *engineCollector) Collect(ch chan<- prometheus.Metric) {
	// A scrape carries no context, so this is where its deadline starts.
	ctx, cancel := context.WithTimeout(context.Background(), engineProbeBudget)
	defer cancel()

	sources := c.sources()
	for i, reading := range readAll(ctx, sources) {
		c.report(ch, sources[i], reading)
	}
}

// engineReading is what one probe answered, or why it did not.
type engineReading struct {
	state EngineState
	err   error
}

// errNoConnection is the reading of a source with no probe.
var errNoConnection = errors.New("this replica has no connection to the database")

// errNoAnswer is the reading of a probe that had not answered by the deadline.
var errNoAnswer = errors.New("the database did not answer within the scrape's budget")

// readAll reads every source at once, each held to the scrape's deadline on its
// own, and waits no longer than that deadline for any of them. A scrape takes
// as long as the slowest database and never longer than the budget, however
// many environments there are, and a probe that ignores its deadline costs its
// own series rather than the scrape.
func readAll(ctx context.Context, sources []EngineSource) []engineReading {
	type answer struct {
		index   int
		reading engineReading
	}
	readings := make([]engineReading, len(sources))
	// Buffered for every probe, so one that answers after the deadline does
	// not block forever on a channel nobody reads.
	answers := make(chan answer, len(sources))
	pending := 0
	for i, source := range sources {
		if source.Probe == nil {
			readings[i].err = errNoConnection
			continue
		}
		readings[i].err = errNoAnswer
		pending++
		go func() {
			state, err := source.Probe(ctx)
			answers <- answer{index: i, reading: engineReading{state: state, err: err}}
		}()
	}
	for ; pending > 0; pending-- {
		select {
		case got := <-answers:
			readings[got.index] = got.reading
		case <-ctx.Done():
			// Keep what arrived with the deadline: select picks between two
			// ready cases at random, and a reading that made it should not be
			// lost on a coin flip.
			for {
				select {
				case got := <-answers:
					readings[got.index] = got.reading
				default:
					return readings
				}
			}
		}
	}
	return readings
}

// report sends one database's series: its backlog, or only `up 0` when it
// could not be read, so an alert on the backlog cannot be quieted by the very
// outage it should be reporting.
func (c *engineCollector) report(ch chan<- prometheus.Metric, source EngineSource, reading engineReading) {
	labels := []string{source.Environment, source.EnvironmentName}
	if reading.err != nil {
		if !errors.Is(reading.err, errNoConnection) {
			event := log.Warn().Err(reading.err)
			if source.Environment != "" {
				event = event.Str("environment", source.EnvironmentName).Str("environment_id", source.Environment)
			}
			event.Msg("Could not read the engine's backlog for the metrics endpoint")
		}
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0, labels...)
		return
	}
	state := reading.state
	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1, labels...)
	ch <- prometheus.MustNewConstMetric(c.due, prometheus.GaugeValue, float64(state.DueJobs), labels...)
	ch <- prometheus.MustNewConstMetric(c.oldest, prometheus.GaugeValue, state.OldestDueAge.Seconds(), labels...)
	ch <- prometheus.MustNewConstMetric(c.leaseExpired, prometheus.GaugeValue, float64(state.LeaseExpiredJobs), labels...)
	ch <- prometheus.MustNewConstMetric(c.incidents, prometheus.GaugeValue, float64(state.OpenIncidents), labels...)
}
