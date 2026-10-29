// Package dbpool sizes the database connection pool.
//
// Only SQLite was ever configured, and only to cap it at one connection.
// PostgreSQL, MySQL and SQL Server ran on database/sql's defaults: unlimited
// open connections and two idle ones. Both halves of that hurt under load.
//
// Unlimited open connections means a burst opens as many as it likes. The
// backpressure limiter admits 128 requests at once, the job worker runs its
// own, and the SSE fan-out and rate-limit exchange poll on top — comfortably
// past PostgreSQL's default max_connections of 100, where the failure is not a
// slow query but "too many clients already" for everybody at once.
//
// Two idle connections means the rest are closed the moment a burst subsides
// and reopened, TLS handshake and all, on the next one. That is a per-request
// round trip that a profile attributes to the driver rather than to the query.
package dbpool

import (
	"time"

	"github.com/gsoultan/metis/internal/pkg/envvar"
)

const (
	// Sized for the default backpressure ceiling rather than for a guess: a
	// pool far smaller than the admitted concurrency turns into a queue nobody
	// can see, and one far larger just moves the contention into the database.
	defaultMaxOpenConns = 25
	defaultConnLifetime = 30 * time.Minute
	defaultConnIdleTime = 5 * time.Minute

	envMaxOpenConns = "METIS_DB_MAX_OPEN_CONNS"
	envMaxIdleConns = "METIS_DB_MAX_IDLE_CONNS"
	envConnLifetime = "METIS_DB_CONN_MAX_LIFETIME"
	envConnIdleTime = "METIS_DB_CONN_MAX_IDLE_TIME"
)

// Settings is the resolved pool configuration.
type Settings struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// Resolve reads the pool settings for a dialect.
//
// SQLite is not configurable here and never gets more than one connection: it
// is a single-writer file database, and a second pooled connection inside a
// transaction deadlocks on a lock upgrade that busy_timeout does not cover.
func Resolve(dialect string) Settings {
	if dialect == "sqlite" {
		return Settings{MaxOpenConns: 1, MaxIdleConns: 1}
	}

	maxOpen := envInt(envMaxOpenConns, defaultMaxOpenConns)
	// Idle defaults to open. Holding fewer idle than the pool will routinely
	// use is what makes a steady workload reconnect for no reason.
	maxIdle := envInt(envMaxIdleConns, maxOpen)
	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}
	return Settings{
		MaxOpenConns:    maxOpen,
		MaxIdleConns:    maxIdle,
		ConnMaxLifetime: envDuration(envConnLifetime, defaultConnLifetime),
		ConnMaxIdleTime: envDuration(envConnIdleTime, defaultConnIdleTime),
	}
}

func envInt(name string, fallback int) int {
	raw := envvar.Get(name)
	if raw == "" {
		return fallback
	}
	n := 0
	for _, c := range raw {
		if c < '0' || c > '9' {
			return fallback
		}
		n = n*10 + int(c-'0')
		if n > 1_000_000 {
			return fallback
		}
	}
	if n <= 0 {
		return fallback
	}
	return n
}

func envDuration(name string, fallback time.Duration) time.Duration {
	raw := envvar.Get(name)
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		return fallback
	}
	return d
}
