package impl

import (
	"context"

	amqp "github.com/rabbitmq/amqp091-go"
)

// queueChannel is what the inbound consumer needs from a channel to read a
// queue.
type queueChannel interface {
	QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error)
	Qos(prefetchCount, prefetchSize int, global bool) error
	ConsumeWithContext(ctx context.Context, queue, consumer string, autoAck, exclusive, noLocal, noWait bool, args amqp.Table) (<-chan amqp.Delivery, error)
}
