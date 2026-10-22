package impl

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
)

// OutgoingWebhookObserver queues the events a project's outgoing webhooks
// asked for, in the transaction that raised them.
//
// Writing the queue in that transaction is the whole design. A delivery
// exists exactly when the step that caused it committed: a step that rolled
// back is never announced, and one that committed is announced even if this
// process dies a moment later, because the row is already there for the
// worker to find. Posting from here, as WebhookObserver does, can only ever
// be one or the other.
type OutgoingWebhookObserver struct {
	queue contracts.OutgoingWebhookQueue
	// attempt runs the write under a savepoint, so a write that fails does not
	// abort the transaction of the step around it.
	attempt func(ctx context.Context, fn func(ctx context.Context) error) error
}

// NewOutgoingWebhookObserver creates the observer. attempt is the unit of
// work's.
func NewOutgoingWebhookObserver(queue contracts.OutgoingWebhookQueue, attempt func(ctx context.Context, fn func(ctx context.Context) error) error) *OutgoingWebhookObserver {
	return &OutgoingWebhookObserver{queue: queue, attempt: attempt}
}

// OnEvent queues the event for every webhook of its project that wants it.
//
// A failure to queue does not fail the step. It is the audit trail's
// trade-off (AuditLogObserver): the step is the business commitment, and
// refusing an approval because a receiver's queue could not be written would
// put an integration in charge of the process. It is logged as an error with
// everything needed to tell the receiver what it missed.
func (o *OutgoingWebhookObserver) OnEvent(ctx context.Context, event entities.ProcessEvent) {
	outgoing, wanted := entities.OutgoingEventFor(event.Type)
	if !wanted {
		return
	}
	projectID := event.ProjectOf()
	if projectID == uuid.Nil {
		return
	}
	body, err := outgoingBody(outgoing, projectID, event)
	if err != nil {
		logUnqueued(err, outgoing, projectID, event)
		return
	}
	err = o.attempt(ctx, func(ctx context.Context) error {
		_, err := o.queue.Enqueue(ctx, projectID, outgoing, body)
		return err
	})
	if err != nil {
		logUnqueued(err, outgoing, projectID, event)
	}
}

func outgoingBody(outgoing entities.OutgoingWebhookEvent, projectID uuid.UUID, event entities.ProcessEvent) ([]byte, error) {
	eventID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	return json.Marshal(entities.NewOutgoingWebhookPayload(eventID, outgoing, projectID, event))
}

func logUnqueued(err error, outgoing entities.OutgoingWebhookEvent, projectID uuid.UUID, event entities.ProcessEvent) {
	line := log.Error().Err(err).
		Str("event", string(outgoing)).
		Str("project", projectID.String())
	if event.Instance != nil {
		line = line.Str("instance", event.Instance.ID.String())
	}
	line.Msg("An event was not queued for this project's outgoing webhooks; their receivers will not be told of it")
}
