package impl

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/server/domains/entities"
)

// deliveryTimeout bounds delivering one notification across every channel.
const deliveryTimeout = 30 * time.Second

// DeliverAfterCommit delivers once the transaction that stored the
// notification has committed, on a bounded background queue.
//
// After the commit, because a notification about a task a rollback undid is a
// notification about nothing. In the background, because the request that
// created the task is done and should not wait on a mail server. Bounded,
// because a queue that grows without limit turns a mail server that is down
// into a process that runs out of memory: past queued waiting deliveries a
// new one is dropped with a warning, and the notification centre still has it.
//
// Each delivery runs as system work on a context of its own, derived from
// lifetime rather than from the request: the request's context carries a
// transaction that has committed by the time a worker gets to it, and an
// address looked up through it would ask a connection that is closed. The
// workers stop when lifetime ends; what is still queued then stays in the
// notification centre.
func DeliverAfterCommit(lifetime context.Context, afterCommit func(ctx context.Context, fn func()), workers, queued int) DeliveryScheduler {
	queue := make(chan func(context.Context), queued)
	background := entities.WithSystemContext(lifetime)
	for range workers {
		go drainDeliveries(background, queue)
	}
	return func(ctx context.Context, deliver func(context.Context)) {
		afterCommit(ctx, func() {
			select {
			case queue <- deliver:
			default:
				log.Warn().
					Int("waiting", queued).
					Msg("Too many notifications are waiting to be delivered; this one is in the notification centre and nowhere else")
			}
		})
	}
}

func drainDeliveries(background context.Context, queue <-chan func(context.Context)) {
	for {
		select {
		case <-background.Done():
			return
		case deliver := <-queue:
			deliverWithin(background, deliver)
		}
	}
}

func deliverWithin(background context.Context, deliver func(context.Context)) {
	ctx, cancel := context.WithTimeout(background, deliveryTimeout)
	defer cancel()
	deliver(ctx)
}
