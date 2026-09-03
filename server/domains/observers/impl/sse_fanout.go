package impl

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/rs/zerolog/log"
)

// SSEFanout carries SSE events between replicas over the database.
//
// The client registry is a map of open response writers, so it cannot be
// anything but per-process. Before this existed, that meant a browser connected
// to replica A never learned about work done on replica B — its lists simply
// stopped updating, with no error in any log, which is the worst way for a
// feature to fail.
//
// Polling rather than PostgreSQL's LISTEN/NOTIFY, because Metis supports
// SQLite, MySQL and SQL Server too and none of them has an equivalent. A
// portable mechanism that every deployment gets is worth more here than a
// faster one that only one dialect can use.
type SSEFanout struct {
	repo     contracts.BroadcastRepository
	observer *SSEObserver

	// origin identifies this replica, so it can skip the events it published
	// itself — it has already delivered those to its own clients directly.
	origin string

	interval  time.Duration
	batch     int
	retention time.Duration

	// lastID is the high-water mark of what has been delivered here. Only the
	// poll loop touches it, so it needs no lock.
	lastID int64

	// queue holds events waiting to go onto the bus, and dropped counts the
	// ones that did not fit.
	//
	// Publishing used to start a goroutine per event. That is fine while the
	// database keeps up and unbounded when it does not: measured, five thousand
	// events against a slow bus produced five thousand goroutines, each holding
	// a payload and a pending write. The conditions that make the database slow
	// are the conditions that make the engine busy, so the failure arrives when
	// there is least room for it.
	queue   chan string
	dropped atomic.Uint64
}

const (
	// publishQueueDepth is how many events may be waiting for the bus.
	//
	// Deep enough to absorb a burst, shallow enough that a bus which has
	// stopped answering costs a bounded amount of memory rather than an
	// increasing one.
	publishQueueDepth = 1024

	// publishWorkers drain the queue. A handful, because each holds a database
	// connection while it writes and the pool is shared with work that matters
	// more than a UI hint.
	publishWorkers = 4
)

const (
	// defaultFanoutInterval is the delay a browser on another replica sees. It
	// is a compromise: every replica issues one indexed query per tick whether
	// or not anything happened, so shortening it costs queries on an idle
	// system, and lengthening it makes the UI feel stale.
	defaultFanoutInterval = 500 * time.Millisecond

	// defaultFanoutBatch bounds one read. A burst is drained over several
	// ticks rather than in one unbounded query, so a backlog cannot turn into
	// a single enormous result set.
	defaultFanoutBatch = 256

	// defaultFanoutRetention is how long a delivered event stays. The table is
	// a bus, not an audit log — that is what the audit log is for — so rows
	// are kept only long enough to cover a replica that is briefly behind.
	defaultFanoutRetention = 5 * time.Minute

	// pruneEvery is how often the oldest rows are swept. Deliberately much
	// longer than the poll interval: pruning is a write, and doing it every
	// tick would have every replica writing to the same table constantly.
	pruneEvery = 1 * time.Minute
)

// NewSSEFanout wires an observer to the shared bus.
func NewSSEFanout(repo contracts.BroadcastRepository, observer *SSEObserver, origin string) *SSEFanout {
	return &SSEFanout{
		repo:      repo,
		observer:  observer,
		origin:    origin,
		queue:     make(chan string, publishQueueDepth),
		interval:  defaultFanoutInterval,
		batch:     defaultFanoutBatch,
		retention: defaultFanoutRetention,
	}
}

// Start begins publishing this replica's events and delivering everyone else's.
// It returns once the initial position is established; the polling runs until
// ctx is cancelled.
func (f *SSEFanout) Start(ctx context.Context) error {
	// Start from the current end of the bus rather than from zero. Replaying
	// history would flood every browser that connected after a restart with
	// invalidations for work that finished before they arrived.
	latest, err := f.repo.LatestID(ctx)
	if err != nil {
		return err
	}
	f.lastID = latest

	f.observer.PublishVia(f.publish)

	for range publishWorkers {
		go f.publishLoop(ctx)
	}
	go f.loop(ctx)
	return nil
}

// publish puts one encoded event on the bus for other replicas.
//
// It is called on the path that produced the event, so it must not block that
// path on a database write: the goroutine is the point. A failure is logged and
// dropped rather than retried, because the local clients have already been
// served and the UI treats an event as a hint to refetch.
func (f *SSEFanout) publish(payload string) {
	select {
	case f.queue <- payload:
	default:
		// Dropped rather than queued without limit, and counted rather than
		// logged: the moment this happens is the moment the bus is struggling,
		// and a line per dropped event would be a second flood on top of the
		// first. The count is reported by the poll loop instead.
		//
		// Safe to drop because the UI treats an event as a hint to refetch, not
		// as data — the same reason a slow browser is skipped rather than
		// waited for. What is lost is promptness on other replicas.
		f.dropped.Add(1)
	}
}

// publishLoop drains the queue onto the bus.
func (f *SSEFanout) publishLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case payload := <-f.queue:
			f.writeToBus(ctx, payload)
		}
	}
}

func (f *SSEFanout) writeToBus(ctx context.Context, payload string) {
	// Detached from the caller's cancellation on purpose: the event outlives
	// the HTTP request that caused it, and cancelling that request must not
	// cancel telling the other replicas about it. Still bounded by the
	// fan-out's own lifetime through the timeout.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := f.repo.Publish(writeCtx, f.origin, payload); err != nil {
		log.Warn().Err(err).
			Msg("An SSE event was not put on the shared bus; browsers on other replicas will not see it until they refetch.")
	}
}

// reportDrops says how much promptness was lost, once per sweep rather than
// once per event.
func (f *SSEFanout) reportDrops() {
	if dropped := f.dropped.Swap(0); dropped > 0 {
		log.Warn().Uint64("events", dropped).Int("queue_depth", publishQueueDepth).
			Msg("The SSE bus could not keep up, so events were dropped. Browsers on other replicas will not see those changes until they refetch; this replica's own browsers were unaffected.")
	}
}

func (f *SSEFanout) loop(ctx context.Context) {
	ticker := time.NewTicker(f.interval)
	defer ticker.Stop()

	pruner := time.NewTicker(pruneEvery)
	defer pruner.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.drain(ctx)
		case <-pruner.C:
			f.prune(ctx)
			f.reportDrops()
		}
	}
}

// drain delivers everything published since the last tick, in batches.
func (f *SSEFanout) drain(ctx context.Context) {
	for {
		events, err := f.repo.Since(ctx, f.origin, f.lastID, f.batch)
		if err != nil {
			// Left for the next tick. The high-water mark is not advanced, so
			// nothing is skipped by a failed read — it is retried in 500ms.
			log.Warn().Err(err).Msg("Could not read the SSE bus; browsers on this replica may lag by a tick.")
			return
		}
		if len(events) == 0 {
			return
		}
		for _, event := range events {
			f.observer.DeliverFromPeer(event.Payload)
			f.lastID = event.ID
		}
		// A short read means the bus is drained; anything else means there is
		// a backlog worth continuing through rather than waiting a tick for.
		if len(events) < f.batch {
			return
		}
	}
}

func (f *SSEFanout) prune(ctx context.Context) {
	// Every replica prunes. They will race, and that is fine: the loser deletes
	// nothing because the rows are already gone. Electing a pruner would mean
	// running a leader election to save a no-op DELETE.
	if _, err := f.repo.Prune(ctx, time.Now().UTC().Add(-f.retention)); err != nil {
		log.Warn().Err(err).Msg("Could not prune the SSE bus; it will be retried.")
	}
}
