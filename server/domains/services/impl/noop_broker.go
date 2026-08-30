package impl

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// NoOpBroker is a Null Object implementation of MessageBrokerAdapter.
// It silently discards all published messages and never delivers any subscribed
// messages. Safe to use in local or test deployments where a real broker is not
// available. Replace with a Kafka, RabbitMQ, or NATS implementation in production.
type NoOpBroker struct{}

// NewNoOpBroker creates a new NoOpBroker.
func NewNoOpBroker() contracts.MessageBrokerAdapter {
	return &NoOpBroker{}
}

func (b *NoOpBroker) Publish(_ context.Context, _ string, _ entities.BrokerMessage) error {
	return nil
}

func (b *NoOpBroker) Subscribe(_ context.Context, _ string, _ contracts.BrokerMessageHandler) error {
	return nil
}
