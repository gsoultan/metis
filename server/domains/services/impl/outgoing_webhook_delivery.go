package impl

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/repositories/contracts"
)

// The outgoing webhook worker: posts what the queue says is owed.
//
// The queue is written in the transaction of the step that raised each event,
// so this never announces a step that rolled back. It claims due deliveries by
// pushing their next attempt past a lease, posts each outside any transaction,
// and records what came of it. A worker that dies mid-post loses nothing: the
// lease runs out and the delivery is due again. That makes delivery at least
// once, which is why every attempt carries the same X-Delivery-Id.

// outgoingWebhookRetention is how long a delivery stays in the log. Long
// enough to answer "did they get it?" about last month; the queue holds an
// event for a day at most, so nothing still owed is ever removed.
const outgoingWebhookRetention = 30 * 24 * time.Hour

// maxClaimRoundsPerTick bounds how many full rounds one tick drains, as the
// job worker's does: a queue that is permanently full must not turn the poll
// into a loop that never yields.
const maxClaimRoundsPerTick = 20

type outgoingWebhookDeliverer struct {
	queue    contracts.OutgoingWebhookQueue
	sender   *outgoingWebhookSender
	settings OutgoingWebhookSettings
	now      func() time.Time
}

// NewOutgoingWebhookDeliverer creates the worker that posts the queue.
func NewOutgoingWebhookDeliverer(queue contracts.OutgoingWebhookQueue, settings OutgoingWebhookSettings) servicecontracts.OutgoingWebhookDeliverer {
	return &outgoingWebhookDeliverer{
		queue:    queue,
		sender:   newOutgoingWebhookSender(settings.Timeout),
		settings: settings,
		now:      time.Now,
	}
}

func (d *outgoingWebhookDeliverer) StartOutgoingWebhookDeliveries(ctx context.Context) {
	// Every tenant's deliveries, as the job worker does every tenant's jobs.
	ctx = entities.WithSystemContext(ctx)
	log.Info().
		Int("workers", d.settings.Workers).
		Dur("poll", d.settings.PollInterval).
		Dur("timeout", d.settings.Timeout).
		Msg("Outgoing webhook worker started")

	go func() {
		ticker := time.NewTicker(d.settings.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// A panic here would end the ticker, and with it every delivery.
				_ = runRecovered("outgoing webhook poll", func() error {
					d.drain(ctx)
					return nil
				})
			}
		}
	}()
}

func (d *outgoingWebhookDeliverer) ForgetOldOutgoingDeliveries(ctx context.Context) (int64, error) {
	return d.queue.ForgetBefore(ctx, d.now().Add(-outgoingWebhookRetention))
}

// drain posts due deliveries until a round comes back short of full.
func (d *outgoingWebhookDeliverer) drain(ctx context.Context) {
	for range maxClaimRoundsPerTick {
		claimed, err := d.round(ctx)
		if err != nil {
			// Logged here because the ticker cannot return it: a worker that
			// has stopped claiming must not look like one with nothing to do.
			log.Error().Err(err).Msg("A round of outgoing webhook deliveries could not be claimed")
			return
		}
		if claimed < d.settings.Workers || ctx.Err() != nil {
			return
		}
	}
}

// round claims as many deliveries as there are workers and posts them in
// parallel, returning once every one is settled. Claiming no more than that is
// what bounds the goroutines: a round is never more than Workers wide.
func (d *outgoingWebhookDeliverer) round(ctx context.Context) (int, error) {
	due, err := d.queue.ClaimDue(ctx, d.now(), d.settings.Lease, d.settings.Workers)
	if err != nil {
		return 0, err
	}
	var wg sync.WaitGroup
	for _, delivery := range due {
		wg.Go(func() { d.deliver(ctx, delivery) })
	}
	wg.Wait()
	return len(due), nil
}

// deliver makes one attempt and records it.
func (d *outgoingWebhookDeliverer) deliver(ctx context.Context, delivery entities.OutgoingWebhookDelivery) {
	attempt := entities.OutgoingWebhookAttempt{}
	if err := runRecovered("outgoing webhook delivery", func() error {
		attempt = d.sender.post(ctx, delivery, d.now())
		return nil
	}); err != nil {
		attempt = entities.OutgoingWebhookAttempt{Error: "the attempt failed inside Metis", At: d.now()}
	}
	settled := settle(delivery, attempt)
	if err := d.queue.Settle(ctx, settled, attempt); err != nil {
		// Not lost: unsettled, the delivery is due again when its lease runs
		// out, and is posted again under the same id.
		log.Error().Err(err).
			Str("delivery", delivery.ID.String()).
			Msg("Could not record an outgoing webhook attempt; it will be made again")
		return
	}
	logSettled(settled, attempt)
}

// settle decides where a delivery stands after an attempt: delivered, due
// again after the schedule's wait, or failed for good.
func settle(delivery entities.OutgoingWebhookDelivery, attempt entities.OutgoingWebhookAttempt) entities.OutgoingWebhookDelivery {
	settled := delivery
	settled.NextAttemptAt = attempt.At
	switch {
	case attempt.Accepted():
		settled.Status = entities.DeliveryDelivered
	case attempt.Permanent || delivery.Attempts >= maxOutgoingAttempts():
		settled.Status = entities.DeliveryFailed
	default:
		settled.Status = entities.DeliveryPending
		settled.NextAttemptAt = attempt.At.Add(jittered(outgoingRetrySchedule[delivery.Attempts-1]))
	}
	return settled
}

func logSettled(delivery entities.OutgoingWebhookDelivery, attempt entities.OutgoingWebhookAttempt) {
	event := log.Debug()
	switch delivery.Status {
	case entities.DeliveryFailed:
		event = log.Warn()
	case entities.DeliveryPending:
		event = log.Info()
	}
	event.
		Str("delivery", delivery.ID.String()).
		Str("webhook", delivery.Webhook.ID.String()).
		Str("event", string(delivery.Event)).
		Int("attempt", delivery.Attempts).
		Int("status", attempt.StatusCode).
		Str("error", attempt.Error).
		Str("outcome", string(delivery.Status)).
		Msg("Outgoing webhook attempt")
}
