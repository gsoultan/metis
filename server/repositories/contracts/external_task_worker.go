package contracts

import (
	"context"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// ExternalTaskWorkerRepository keeps the workers that fetch external tasks: a
// row per worker on a topic in an organization, which each fetch brings up to
// date.
type ExternalTaskWorkerRepository interface {
	// Seen records that a worker fetched a topic at a moment, for the
	// caller's organization. A worker seen again within a few seconds is not
	// written again: a fetch is the heartbeat, and one poll is enough of it.
	// System work, which has no organization, records nothing.
	Seen(ctx context.Context, topic, workerID, version string, at time.Time) error
	// Topics lists the caller's organization's topics: each that has tasks
	// open or a worker heard from, with its workers, as of now.
	Topics(ctx context.Context, now time.Time) ([]entities.ExternalTaskTopic, error)
	// CountUnserved counts, across every organization, the topics with tasks
	// waiting and no live worker. System work only.
	CountUnserved(ctx context.Context, now time.Time) (int64, error)
	// ForgetIdle removes the workers not heard from since before. System work
	// only.
	ForgetIdle(ctx context.Context, before time.Time) (int64, error)
}
