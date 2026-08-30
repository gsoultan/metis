package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// BrokerPublisher defines the write side of the message broker Strategy.
// Implementations publish domain events to an external broker topic so that
// downstream systems (or other goBPM instances) can react to process events.
type BrokerPublisher interface {
	Publish(ctx context.Context, topic string, msg entities.BrokerMessage) error
}
