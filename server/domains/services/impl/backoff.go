package impl

import (
	"math"
	"math/rand/v2"
	"time"
)

// Retry timing.
//
// The engine retried on a linear schedule — one minute, then two, then three.
// Linear is the wrong shape for the failure it exists to survive. A downstream
// that has fallen over is not helped by a client that keeps coming back on a
// fixed cadence, and every instance that hit the same outage retried in step
// with every other, so recovery arrived as a synchronised wave of traffic at
// exactly the moment the service was least able to take it.
//
// Exponential spreads attempts out as the failure persists. Jitter breaks the
// synchronisation: two thousand instances that failed together come back at two
// thousand different moments.
const (
	// baseRetryDelay is how long the first retry waits. A transient failure —
	// a connection reset, a leader election, a pod being replaced — is usually
	// over well inside a minute.
	baseRetryDelay = 30 * time.Second

	// maxRetryDelay bounds the growth. Past this the delay stops being a way to
	// let a service recover and starts being a way to lose a business
	// commitment in a queue.
	maxRetryDelay = 15 * time.Minute

	// jitterFraction is how much of the delay is randomised. Full jitter — a
	// uniform pick over the whole interval — spreads best but makes the first
	// retry arrive anywhere from immediately to the full delay, which reads as
	// erratic to anyone watching one instance. A quarter is enough to break a
	// thundering herd while keeping the schedule recognisable.
	jitterFraction = 0.25
)

// retryDelay returns how long to wait before attempt number `retries`.
//
// retries is the count already spent, so the first retry passes 1.
func retryDelay(retries int) time.Duration {
	if retries < 1 {
		retries = 1
	}

	// Shifting rather than math.Pow, and capped before the shift: at 63 the
	// shift overflows, and a definition can set its own MaxRetries.
	delay := maxRetryDelay
	if retries <= 32 {
		grown := float64(baseRetryDelay) * math.Pow(2, float64(retries-1))
		if grown < float64(maxRetryDelay) {
			delay = time.Duration(grown)
		}
	}

	spread := float64(delay) * jitterFraction
	// Centred on the delay: the wait is somewhere in [delay-spread, delay+spread],
	// so the average schedule is the one the constants describe.
	offset := (rand.Float64()*2 - 1) * spread
	jittered := time.Duration(float64(delay) + offset)
	if jittered < time.Second {
		return time.Second
	}
	return jittered
}
