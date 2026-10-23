package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The server's own gauges, read from its metrics endpoint rather than from the
// operating system: they are what a dashboard shows in production, they work
// against a server on another host or in a container, and they report the Go
// heap separately from the resident set.
const (
	metricRSS          = "process_resident_memory_bytes"
	metricCPU          = "process_cpu_seconds_total"
	metricHeap         = "go_memstats_heap_inuse_bytes"
	metricGoroutines   = "go_goroutines"
	metricPoolWaits    = "metis_db_pool_acquire_waits_total"
	metricPoolWaitTime = "metis_db_pool_acquire_wait_seconds_total"
)

var sampledMetrics = []string{metricRSS, metricCPU, metricHeap, metricGoroutines, metricPoolWaits, metricPoolWaitTime}

// sampler reads the server's gauges once a second for the length of a run, so
// the report can give the peak as well as the start and the end.
type sampler struct {
	url     string
	http    *http.Client
	mu      sync.Mutex
	samples []serverSample
}

const sampleInterval = time.Second

func (s *sampler) run(ctx context.Context) {
	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()
	for {
		s.take(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *sampler) take(ctx context.Context) {
	values, err := s.read(ctx)
	if err != nil {
		return
	}
	s.mu.Lock()
	s.samples = append(s.samples, serverSample{at: time.Now(), values: values})
	s.mu.Unlock()
}

// read fetches and parses the exposition text.
func (s *sampler) read(ctx context.Context) (map[string]float64, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metrics answered %d", resp.StatusCode)
	}
	values := make(map[string]float64, len(sampledMetrics))
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		name, value, ok := parseSeries(scanner.Text())
		if ok && isSampled(name) {
			// Summed across labels: the pool metrics carry pool="storm".
			values[name] += value
		}
	}
	return values, scanner.Err()
}

// parseSeries reads one line of exposition text: a name, optional labels, and
// a value. Comments and anything unparseable are skipped.
func parseSeries(line string) (string, float64, bool) {
	if strings.HasPrefix(line, "#") {
		return "", 0, false
	}
	series, raw, ok := strings.Cut(line, " ")
	if labels := strings.IndexByte(series, '{'); labels >= 0 {
		series = series[:labels]
		_, raw, ok = strings.Cut(line, "} ")
	}
	if !ok {
		return "", 0, false
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return series, value, err == nil
}

func isSampled(name string) bool {
	for _, wanted := range sampledMetrics {
		if name == wanted {
			return true
		}
	}
	return false
}

func (s *sampler) usage() (usage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.samples) < 2 {
		return usage{}, false
	}
	first, last := s.samples[0], s.samples[len(s.samples)-1]
	out := usage{
		samples:     len(s.samples),
		rssStart:    first.values[metricRSS],
		rssEnd:      last.values[metricRSS],
		cpuSeconds:  last.values[metricCPU] - first.values[metricCPU],
		poolWaits:   last.values[metricPoolWaits] - first.values[metricPoolWaits],
		poolWaitSec: last.values[metricPoolWaitTime] - first.values[metricPoolWaitTime],
	}
	for _, sample := range s.samples {
		out.rssPeak = max(out.rssPeak, sample.values[metricRSS])
		out.heapPeak = max(out.heapPeak, sample.values[metricHeap])
		out.goroutinesPeak = max(out.goroutinesPeak, sample.values[metricGoroutines])
	}
	if wall := last.at.Sub(first.at).Seconds(); wall > 0 {
		out.cpuCores = out.cpuSeconds / wall
	}
	return out, true
}
