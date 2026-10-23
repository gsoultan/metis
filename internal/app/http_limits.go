package app

import (
	"strconv"
	"strings"

	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/rs/zerolog/log"
)

// HTTP admission limits.
//
// These were compile-time constants, which is the shape the perf profile in
// AGENTS.md vetoes: a ceiling that no amount of hardware can raise short of a
// rebuild. Measured with cmd/metisload, they decide what 1,000 concurrent
// clients see before anything else does. 128 running and 256 waiting is 384
// admitted, so the 385th concurrent call is refused with a 503 however idle
// the database is; and 240 calls a minute is four a second per address, which
// one integration posting work from one host spends in its first second.
//
// The defaults are unchanged. What changes is that an installation sized for
// more can say so, and that the values in force are logged at start-up.
const (
	envHTTPRateLimit   = "METIS_HTTP_RATE_LIMIT"
	envHTTPMaxInFlight = "METIS_HTTP_MAX_IN_FLIGHT"
	envHTTPMaxQueued   = "METIS_HTTP_MAX_QUEUED"

	// maxHTTPLimit bounds every setting. Each admitted call is a goroutine and
	// a buffered request; a typo with three extra zeros should not be the
	// thing that lets a burst take the process's memory.
	maxHTTPLimit = 100_000
)

// httpLimits is the resolved admission policy for the API.
type httpLimits struct {
	// requestsPerMinute is each client address's allowance.
	requestsPerMinute int
	// maxInFlight calls run at once; maxQueued more wait for a slot, and any
	// beyond that are refused with 503 and a Retry-After.
	maxInFlight int
	maxQueued   int
}

// resolveHTTPLimits reads the limits from the environment. A value that is
// missing, not a whole number, below one or past maxHTTPLimit falls back to
// the default, and says so: a limit of zero would refuse every call, and one
// that silently fell back would leave the operator believing the raised value
// was in force.
func resolveHTTPLimits() httpLimits {
	limits := httpLimits{
		requestsPerMinute: httpLimitFromEnv(envHTTPRateLimit, defaultHTTPMaxRequestsPerLimit),
		maxInFlight:       httpLimitFromEnv(envHTTPMaxInFlight, defaultHTTPMaxInFlightRequests),
		maxQueued:         httpLimitFromEnv(envHTTPMaxQueued, defaultHTTPMaxQueuedRequests),
	}
	log.Info().
		Int("rate_limit_per_minute", limits.requestsPerMinute).
		Int("max_in_flight", limits.maxInFlight).
		Int("max_queued", limits.maxQueued).
		Msg("HTTP admission limits")
	return limits
}

func httpLimitFromEnv(name string, fallback int) int {
	raw := strings.TrimSpace(envvar.Get(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 1 || value > maxHTTPLimit {
		log.Warn().
			Str("env", name).
			Str("value", raw).
			Int("using", fallback).
			Msg("Ignoring an HTTP limit that is not a whole number between 1 and 100000")
		return fallback
	}
	return value
}
