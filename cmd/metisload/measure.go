package main

import (
	"context"
	"net/http"
	"time"
)

// result is everything one run measured.
type result struct {
	users      int
	elapsed    time.Duration
	iterations int64
	summaries  []summary
	failures   map[string]int
	server     usage
	hasServer  bool
	before     snapshot
	after      snapshot
	hasStorage bool
}

// measure runs the load for the configured duration and gathers the result.
func measure(ctx context.Context, o options, token string, transport *http.Transport, probe *storageProbe) (*result, error) {
	out := &result{users: o.users}
	if probe != nil {
		before, err := probe.snapshot(ctx)
		if err != nil {
			return nil, err
		}
		out.before = before
	}

	sampleCtx, stopSampling := context.WithCancel(ctx)
	sampled := make(chan struct{})
	gauges := &sampler{url: o.metrics, http: &http.Client{Timeout: 5 * time.Second}}
	go func() {
		defer close(sampled)
		if o.metrics != "" {
			gauges.run(sampleCtx)
		}
	}()

	load := &runner{
		base: o.base, token: token, projectID: o.projectID, users: o.users,
		transport: transport, timeout: o.timeout, think: o.think, record: newRecorder(),
	}
	loadCtx, cancel := context.WithTimeout(ctx, o.duration)
	began := time.Now()
	load.run(loadCtx)
	out.elapsed = time.Since(began)
	cancel()
	gauges.take(ctx)
	stopSampling()
	<-sampled

	out.iterations = load.iterations.Load()
	for _, operation := range operations {
		out.summaries = append(out.summaries, load.record.summarize(operation))
	}
	out.failures = load.record.failureCounts()
	out.server, out.hasServer = gauges.usage()
	if probe != nil {
		after, err := probe.snapshot(ctx)
		if err != nil {
			return nil, err
		}
		out.after, out.hasStorage = after, true
	}
	return out, nil
}
