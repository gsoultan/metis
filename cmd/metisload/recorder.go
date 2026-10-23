package main

import (
	"errors"
	"slices"
	"sync"
	"time"
)

// recorder keeps every latency of every operation, and why each failure failed.
//
// Every sample is kept rather than bucketed: a run is minutes long and a few
// hundred thousand durations are a few megabytes, and an exact p99 is worth
// more here than the memory.
type recorder struct {
	mu        sync.Mutex
	latencies map[string][]time.Duration
	failures  map[string]map[string]int
}

func newRecorder() *recorder {
	return &recorder{
		latencies: make(map[string][]time.Duration),
		failures:  make(map[string]map[string]int),
	}
}

// observe records one call. A failed call's latency is not counted: a 503 that
// answers in a microsecond would otherwise make an overloaded server look fast.
func (r *recorder) observe(operation string, took time.Duration, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		r.latencies[operation] = append(r.latencies[operation], took)
		return
	}
	if r.failures[operation] == nil {
		r.failures[operation] = make(map[string]int)
	}
	r.failures[operation][failureClass(err)]++
}

// failureClass names a failure by what an operator would do about it.
func failureClass(err error) string {
	var refused *apiError
	if !errors.As(err, &refused) {
		return "transport: " + truncateClass(err.Error())
	}
	switch {
	case refused.status == 429:
		return "429 rate limited"
	case refused.status == 503:
		return "503 shed by backpressure"
	case refused.status >= 500:
		return "5xx: " + truncateClass(refused.body)
	default:
		return "refused: " + truncateClass(refused.body)
	}
}

const classLimit = 90

func truncateClass(text string) string {
	if len(text) > classLimit {
		return text[:classLimit]
	}
	return text
}

func (r *recorder) summarize(operation string) summary {
	r.mu.Lock()
	defer r.mu.Unlock()
	samples := slices.Clone(r.latencies[operation])
	slices.Sort(samples)
	out := summary{operation: operation, ok: len(samples)}
	for _, count := range r.failures[operation] {
		out.failed += count
	}
	if len(samples) == 0 {
		return out
	}
	out.p50 = percentile(samples, 0.50)
	out.p95 = percentile(samples, 0.95)
	out.p99 = percentile(samples, 0.99)
	out.max = samples[len(samples)-1]
	return out
}

func percentile(sorted []time.Duration, q float64) time.Duration {
	index := int(float64(len(sorted)-1) * q)
	return sorted[index]
}

// failureCounts returns every failure class with its count, across operations.
func (r *recorder) failureCounts() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]int)
	for operation, classes := range r.failures {
		for class, count := range classes {
			out[operation+" — "+class] += count
		}
	}
	return out
}
