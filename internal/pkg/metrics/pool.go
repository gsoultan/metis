package metrics

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// poolCollector reports a connection pool's state at scrape time.
//
// An exhausted pool is how a slow query becomes an outage: every request
// waits for a connection, the waits hold the backpressure slots, and the API
// answers 503 while the database itself looks idle. The pool is the one place
// that shows the queue forming, before anything has failed.
type poolCollector struct {
	stat func() *pgxpool.Stat

	connections, max, acquires, waits, waitSeconds, canceled *prometheus.Desc
}

// NewPoolCollector reports the pool stat reads, labelled with the pool's name.
func NewPoolCollector(name string, stat func() *pgxpool.Stat) prometheus.Collector {
	labels := prometheus.Labels{"pool": name}
	return &poolCollector{
		stat: stat,
		connections: prometheus.NewDesc("metis_db_pool_connections",
			"Connections by state: acquired (in use), idle, or being opened.", []string{"state"}, labels),
		max: prometheus.NewDesc("metis_db_pool_max_connections",
			"The most connections the pool will open.", nil, labels),
		acquires: prometheus.NewDesc("metis_db_pool_acquires_total",
			"Connections handed out.", nil, labels),
		waits: prometheus.NewDesc("metis_db_pool_acquire_waits_total",
			"Acquires that found no idle connection and had to wait for one.", nil, labels),
		waitSeconds: prometheus.NewDesc("metis_db_pool_acquire_wait_seconds_total",
			"Time spent waiting by the acquires that had to.", nil, labels),
		canceled: prometheus.NewDesc("metis_db_pool_canceled_acquires_total",
			"Acquires given up on before a connection came free — a request that timed out waiting.", nil, labels),
	}
}

func (c *poolCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.connections
	ch <- c.max
	ch <- c.acquires
	ch <- c.waits
	ch <- c.waitSeconds
	ch <- c.canceled
}

func (c *poolCollector) Collect(ch chan<- prometheus.Metric) {
	s := c.stat()
	if s == nil {
		return
	}
	ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(s.AcquiredConns()), "acquired")
	ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(s.IdleConns()), "idle")
	ch <- prometheus.MustNewConstMetric(c.connections, prometheus.GaugeValue, float64(s.ConstructingConns()), "constructing")
	ch <- prometheus.MustNewConstMetric(c.max, prometheus.GaugeValue, float64(s.MaxConns()))
	ch <- prometheus.MustNewConstMetric(c.acquires, prometheus.CounterValue, float64(s.AcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.waits, prometheus.CounterValue, float64(s.EmptyAcquireCount()))
	ch <- prometheus.MustNewConstMetric(c.waitSeconds, prometheus.CounterValue, s.EmptyAcquireWaitTime().Seconds())
	ch <- prometheus.MustNewConstMetric(c.canceled, prometheus.CounterValue, float64(s.CanceledAcquireCount()))
}
