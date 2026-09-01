package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// BrokerMessageHandler is the callback invoked when a message arrives on a subscribed topic.
type BrokerMessageHandler func(msg entities.BrokerMessage)

// BrokerSubscriber defines the read side of the message broker Strategy.
// Implementations subscribe to broker topics and invoke the handler for each
// incoming message, enabling event-driven correlation to waiting process tokens.
type BrokerSubscriber interface {
	Subscribe(ctx context.Context, topic string, handler BrokerMessageHandler) error
}
