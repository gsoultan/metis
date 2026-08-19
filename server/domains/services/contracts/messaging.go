package contracts

import (
	"context"

	"github.com/google/uuid"
)

// MessagingService manages external messaging integrations like RabbitMQ.
type MessagingService interface {
	// StartBridge starts a background worker that polls for external tasks and forwards them to a message queue.
	StartBridge(ctx context.Context, projectID uuid.UUID, topic string, rabbitURL string, exchange string, routingKey string) error

	// StartInboundConsumer starts a background worker that listens to a message queue and triggers engine messages.
	StartInboundConsumer(ctx context.Context, projectID uuid.UUID, rabbitURL string, queueName string, messageName string) error

	// StopAll stops all active background messaging workers.
	StopAll()
}
