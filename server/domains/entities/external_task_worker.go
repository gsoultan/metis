package entities

import (
	"context"
	"time"
)

// WorkerLiveWindow is how recently a worker must have fetched to count as
// serving its topic. A worker long-polls, so a live one asks far more often
// than this; one that has not asked for this long has stopped, or cannot
// reach the engine.
const WorkerLiveWindow = 2 * time.Minute

// WorkerForgottenAfter is how long a worker that stopped fetching is still
// listed. Long enough to answer "who used to serve this topic" after a
// weekend; short enough that worker ids made fresh at each start, as a
// container's hostname is, do not pile up.
const WorkerForgottenAfter = 7 * 24 * time.Hour

// MaxWorkerVersionLength bounds what a worker may say it is. A version is a
// label for people, so it is cut nowhere: a longer one is refused.
const MaxWorkerVersionLength = 64

// ExternalTaskWorker is a worker that has fetched a topic, as the engine last
// heard from it.
type ExternalTaskWorker struct {
	WorkerID string `json:"worker_id"`
	// Version is what the worker said it is; empty when it said nothing.
	Version string `json:"version,omitempty"`
	// LastSeenAt is its last fetch. Nil for a worker holding a lock it took
	// before workers were recorded, or that has since been forgotten.
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	// Live is whether it fetched within WorkerLiveWindow.
	Live bool `json:"live"`
	// Locked is how many of the topic's tasks it holds now.
	Locked int64 `json:"locked"`
}

// ExternalTaskTopic is one topic an organization's external tasks wait on,
// with the workers that serve it.
type ExternalTaskTopic struct {
	Topic string `json:"topic"`
	// Waiting is how many tasks are on offer: no lock on them, and tries
	// left. Tasks waiting out a retry, or held by an incident, are not.
	Waiting int64 `json:"waiting"`
	// Locked is how many a worker holds now.
	Locked  int64                `json:"locked"`
	Workers []ExternalTaskWorker `json:"workers"`
	// Unserved is tasks waiting with no live worker: they will wait until one
	// starts, and nothing else will tell anybody.
	Unserved bool `json:"unserved"`
}

type workerVersionKey struct{}

// WithWorkerVersion carries what a fetching worker says it is, from the
// transport that read it to the service that records the worker.
func WithWorkerVersion(ctx context.Context, version string) context.Context {
	if version == "" {
		return ctx
	}
	return context.WithValue(ctx, workerVersionKey{}, version)
}

// WorkerVersionFrom is the version WithWorkerVersion carried, or empty.
func WorkerVersionFrom(ctx context.Context) string {
	if version, ok := ctx.Value(workerVersionKey{}).(string); ok {
		return version
	}
	return ""
}
