package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// MessagingService manages external messaging integrations like RabbitMQ.
type MessagingService interface {
	// StartBridge starts a background worker that polls for external tasks and forwards them to a message queue.
	//
	// lockDuration is how long each task it publishes stays locked to it: the
	// downstream worker's budget, its time waiting on the queue included,
	// unless the worker extends the lock with the task id and the worker id
	// the message carries. A task still open when it runs out is published
	// again.
	StartBridge(ctx context.Context, projectID uuid.UUID, topic string, rabbitURL string, exchange string, routingKey string, lockDuration time.Duration) error

	// StartInboundConsumer starts a background worker that listens to a message queue and triggers engine messages.
	StartInboundConsumer(ctx context.Context, projectID uuid.UUID, rabbitURL string, queueName string, messageName string) error

	// StopAll stops all active background messaging workers.
	StopAll()
}
