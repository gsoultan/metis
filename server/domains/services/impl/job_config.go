package impl

import (
	"time"

	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/internal/pkg/httpclient"
	"github.com/rs/zerolog/log"
)

// Job worker sizing.
//
// These were compile-time constants: five workers, a two-second tick, and one
// claim of at most five jobs per tick. That is a ceiling of about 2.5 jobs a
// second per replica no matter what the machine or the database can do, and no
// way to raise it short of a rebuild. It also meant a burst of a thousand timers
// drained at a fixed rate while the pool sat mostly idle between ticks.
//
// The interval is also a latency floor: a service task enqueued just after a
// tick waits the better part of it before anything looks. Two seconds is a long
// time to a process that is otherwise instant.
const (
	// defaultJobWorkers is deliberately close to the database pool's default of
	// 25 rather than to a CPU count: a job's time is spent waiting on the
	// database and on partners, not computing. Above the pool size, workers
	// queue on connections instead of doing work.
	defaultJobWorkers = 10
	// defaultJobPollInterval is how long an idle worker waits before looking
	// again. It bounds how late the *first* job of a quiet period starts;
	// once work exists the drain loop keeps claiming without waiting.
	defaultJobPollInterval = 2 * time.Second
	// defaultJobLease is how long a claim is held before another worker may
	// take the job. Long enough to cover an outbound call's own 30s ceiling
	// several times over, so a slow partner does not cause double execution.
	defaultJobLease = 5 * time.Minute

	envJobWorkers      = "METIS_JOB_WORKERS"
	envJobPollInterval = "METIS_JOB_POLL_INTERVAL"
	envJobLease        = "METIS_JOB_LEASE"
)

// JobWorkerSettings is the resolved sizing for the job worker.
type JobWorkerSettings struct {
	Workers      int
	PollInterval time.Duration
	Lease        time.Duration
	// RenewEvery is how often a running job's lease is renewed. Zero means a
	// third of Lease; tests set it shorter.
	RenewEvery time.Duration
}

func (w JobWorkerSettings) renewEvery() time.Duration {
	if w.RenewEvery > 0 {
		return w.RenewEvery
	}
	if every := w.Lease / renewalsPerLease; every > 0 {
		return every
	}
	// Settings made in code rather than resolved — tests — may leave Lease
	// zero; a ticker of zero panics.
	return defaultJobLease / renewalsPerLease
}

// ResolveJobWorkerSettings reads the sizing from the environment.
//
// Every value falls back to its default rather than to zero: a typo in a
// deployment must not produce a worker with no threads, an interval of zero
// that spins, or a lease of zero that lets every replica run the same job.
func ResolveJobWorkerSettings() JobWorkerSettings {
	settings := JobWorkerSettings{
		Workers:      envPositiveInt(envJobWorkers, defaultJobWorkers),
		PollInterval: envPositiveDuration(envJobPollInterval, defaultJobPollInterval),
		Lease:        envPositiveDuration(envJobLease, defaultJobLease),
	}

	// A lease shorter than an outbound call's own budget means a job can still
	// be running when another worker is entitled to claim it — which is a
	// duplicate service call, the one thing the idempotency work exists to
	// prevent. Refuse the setting rather than the guarantee.
	if minimum := httpclient.MaxTimeout; settings.Lease < minimum {
		log.Warn().
			Dur("configured", settings.Lease).
			Dur("using", minimum).
			Msg("METIS_JOB_LEASE is shorter than an outbound call may run for; a second worker could claim a job still in flight")
		settings.Lease = minimum
	}
	return settings
}

func envPositiveInt(name string, fallback int) int {
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
		if n > 100_000 {
			return fallback
		}
	}
	if n <= 0 {
		return fallback
	}
	return n
}

func envPositiveDuration(name string, fallback time.Duration) time.Duration {
	raw := envvar.Get(name)
	if raw == "" {
		return fallback
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}
