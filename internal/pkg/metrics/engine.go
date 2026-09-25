package metrics

import (
	"context"
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

// EngineProbe reads the engine's state, once per scrape.
type EngineProbe func(ctx context.Context) (EngineState, error)

// engineProbeBudget bounds one scrape's reads. Well inside a scrape timeout,
// so a slow database shows up as `metis_engine_state_up 0` rather than as a
// scrape that failed and took every other series with it.
const engineProbeBudget = 2 * time.Second

// engineCollector reports the engine's backlog at scrape time.
//
// The HTTP metrics say nothing about the part of the engine nobody is waiting
// on: a job worker that stopped claiming looks exactly like a system with
// nothing to do. These are the series that tell the two apart. They are read
// at scrape time, not counted as work happens, because the question is "how
// far behind is the queue now", which only the database can answer across
// replicas.
type engineCollector struct {
	probe EngineProbe

	up, due, oldest, leaseExpired, incidents *prometheus.Desc
}

// NewEngineCollector reports what probe reads.
func NewEngineCollector(probe EngineProbe) prometheus.Collector {
	return &engineCollector{
		probe: probe,
		up: prometheus.NewDesc("metis_engine_state_up",
			"1 when the engine's backlog could be read at this scrape. The series below are absent while it is 0.", nil, nil),
		due: prometheus.NewDesc("metis_jobs_due",
			"Jobs whose time has come that no worker has claimed.", nil, nil),
		oldest: prometheus.NewDesc("metis_jobs_oldest_due_age_seconds",
			"How long the longest-waiting due job has waited. Grows without bound when no worker is claiming.", nil, nil),
		leaseExpired: prometheus.NewDesc("metis_jobs_lease_expired",
			"Jobs a worker claimed and stopped renewing. The next poll reclaims them, so this should not stay above zero.", nil, nil),
		incidents: prometheus.NewDesc("metis_incidents_open",
			"Incidents nobody has resolved.", nil, nil),
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

	state, err := c.probe(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Could not read the engine's backlog for the metrics endpoint")
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)
	ch <- prometheus.MustNewConstMetric(c.due, prometheus.GaugeValue, float64(state.DueJobs))
	ch <- prometheus.MustNewConstMetric(c.oldest, prometheus.GaugeValue, state.OldestDueAge.Seconds())
	ch <- prometheus.MustNewConstMetric(c.leaseExpired, prometheus.GaugeValue, float64(state.LeaseExpiredJobs))
	ch <- prometheus.MustNewConstMetric(c.incidents, prometheus.GaugeValue, float64(state.OpenIncidents))
}
