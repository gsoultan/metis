package model

import (
	"time"

	"github.com/gsoultan/storm"
)

// ExternalTaskWorker is a worker the engine has heard from: one worker id
// fetching one topic for one organization, and when it last asked for work.
//
// A fetch is the worker's heartbeat. Nothing else says a worker is there: a
// worker with nothing to do holds no task, so the tasks alone cannot tell an
// idle worker from a missing one, and a topic whose tasks wait with nobody
// fetching them looks, from the tasks, like a busy one.
//
// A row lives in the database the tasks it fetched live in, so an
// environment's workers are counted against that environment's tasks.
type ExternalTaskWorker struct {
	storm.Model

	Organization Organization
	Topic        string
	WorkerID     string
	// Version is what the worker says it is, when it says: an SDK's version or
	// a build's, free text.
	Version    *string
	LastSeenAt time.Time
}

func (w *ExternalTaskWorker) Schema(t *storm.Table) {
	t.Name("external_task_workers")
	t.Col(&w.Topic).Size(255)
	t.Col(&w.WorkerID).Size(255)
	t.Col(&w.Version).Size(64)
	// One row per worker on a topic, which a fetch updates in place.
	t.Index(&w.Organization, &w.Topic, &w.WorkerID).Unique().Named("ux_external_task_workers_worker")
	// Which is rewritten only when it is stale or says another version
	// (UpsertWhere).
	t.GuardsUpsert()
	// The sweep that forgets workers not heard from in a week.
	t.Col(&w.LastSeenAt).Index()
	// Which removes a batch at a time (DeleteBatch).
	t.Takes()
}
