package impl

import (
	amqp "github.com/rabbitmq/amqp091-go"
)

// closeWatcher is a connection or channel that says when the broker has
// closed it, and why, and that can be closed.
type closeWatcher interface {
	// NotifyClose registers receiver for the broker's reason, sent once when
	// the broker closes it; receiver is closed with no reason when it is
	// closed on purpose.
	NotifyClose(receiver chan *amqp.Error) chan *amqp.Error
	Close() error
}
