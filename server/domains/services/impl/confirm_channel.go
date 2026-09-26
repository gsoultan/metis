package impl

import (
	"context"

	amqp "github.com/rabbitmq/amqp091-go"
)

// confirmChannel is what a confirmingPublisher needs from a channel: confirm
// mode, word of the messages the broker hands back, and a publish whose
// answer can be waited for.
type confirmChannel interface {
	Confirm(noWait bool) error
	NotifyReturn(receiver chan amqp.Return) chan amqp.Return
	PublishWithDeferredConfirmWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (publishConfirmation, error)
}
