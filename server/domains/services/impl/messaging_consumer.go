package impl

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rs/zerolog"
)

// What a consumer says about a problem, once per cause: see problemLog.
const (
	msgConsumerCouldNotConsume = "A RabbitMQ consumer could not consume from its queue; retrying"
	msgConsumerStopped         = "A RabbitMQ consumer stopped receiving messages from its queue; it will consume again"
)

// errConsumerCancelled is the deliveries stopping on a channel that is still
// open: the broker cancelled the consumer.
var errConsumerCancelled = errors.New("the broker cancelled the consumer, which it does when the queue is deleted; the queue is declared again")

// inboundConsumer reads one queue and correlates each message on it as a BPMN
// message of one project.
type inboundConsumer struct {
	messaging      *messagingService
	link           *brokerLink
	project        uuid.UUID
	queue          string
	message        string
	confirmTimeout time.Duration
	retryIn        time.Duration
	sleep          func(ctx context.Context, delay time.Duration) error
	logger         *zerolog.Logger
	problems       problemLog
}

// run consumes until ctx ends. Each time the deliveries stop it says why and
// consumes again, on a new channel of the connection it has while that is up.
func (c *inboundConsumer) run(ctx context.Context) {
	defer c.link.close()
	for {
		consumed, err := c.consume(ctx)
		if ctx.Err() != nil {
			return
		}
		message := msgConsumerCouldNotConsume
		if consumed {
			message = msgConsumerStopped
		}
		c.problems.event(message, err).Dur("retryIn", c.retryIn).Msg(message)
		if c.sleep(ctx, c.retryIn) != nil {
			return
		}
	}
}

// consume reads the queue until the deliveries stop, and reports whether it
// got as far as consuming, and why they stopped.
func (c *inboundConsumer) consume(ctx context.Context) (bool, error) {
	deliveries, publisher, err := c.subscribe(ctx)
	if err != nil {
		// A channel that failed part-way through being set up is not one to
		// consume on.
		c.link.dropChannel()
		return false, err
	}
	c.problems.working()
	// On every channel, the first and each one after a loss.
	c.logger.Info().Str("deadLetterQueue", c.deadLetterQueue()).Msg("A RabbitMQ consumer is consuming from its queue")

	publishToQueue := func(ctx context.Context, queueName string, message amqp.Publishing) error {
		// The default exchange routes by queue name, and the queue is declared
		// in subscribe, so this is routable — but it still has to be
		// confirmed, or a broker that refused it would look identical to one
		// that took it.
		return publisher.publish(ctx, "", queueName, message)
	}
	for delivery := range deliveries {
		if err := c.handle(ctx, delivery, publishToQueue); errors.Is(err, errConfirmTimeout) {
			// A channel that missed a confirm cannot be trusted with the
			// next one. Closing it hands the broker back everything it has
			// not had an answer about.
			c.link.dropChannel()
			return true, err
		}
	}
	return true, c.whyDeliveriesStopped(ctx)
}

// subscribe opens a channel, declares the queue and its dead-letter queue on
// it, and starts consuming.
func (c *inboundConsumer) subscribe(ctx context.Context) (<-chan amqp.Delivery, *confirmingPublisher, error) {
	ch, _, err := c.link.open()
	if err != nil {
		return nil, nil, err
	}
	if _, err := ch.QueueDeclare(c.queue, true, false, false, false, nil); err != nil {
		return nil, nil, err
	}
	if _, err := ch.QueueDeclare(c.deadLetterQueue(), true, false, false, false, nil); err != nil {
		return nil, nil, err
	}
	// The dead-letter publish has to be accounted for before the message it is
	// standing in for can be acknowledged, so the channel needs confirms.
	publisher, err := newConfirmingPublisher(ch, c.confirmTimeout)
	if err != nil {
		return nil, nil, err
	}
	// Manual acknowledgement, so one message at a time is outstanding and the
	// broker holds the rest.
	if err := ch.Qos(inboundPrefetch, 0, false); err != nil {
		return nil, nil, err
	}
	// autoAck was true, which acknowledges a message the moment the broker
	// hands it over — before anything has looked at it. Every path that tries
	// to preserve a message it could not process was therefore preserving one
	// the broker had already forgotten: if the dead-letter publish failed, or
	// the engine was shutting down, the message was simply gone.
	deliveries, err := ch.ConsumeWithContext(ctx, c.queue, "", false, false, false, false, nil)
	if err != nil {
		return nil, nil, err
	}
	return deliveries, publisher, nil
}

// handle correlates one delivery and settles it with the broker.
func (c *inboundConsumer) handle(ctx context.Context, delivery amqp.Delivery, publishToQueue func(context.Context, string, amqp.Publishing) error) error {
	outcome, err := c.messaging.processInboundDelivery(ctx, c.project, c.queue, c.deadLetterQueue(), c.message, delivery, publishToQueue)
	if err != nil {
		c.logger.Error().Err(err).Str("deadLetterQueue", c.deadLetterQueue()).Msg("Error processing inbound message")
	}
	settleInboundDelivery(delivery, outcome, c.queue)
	return err
}

// whyDeliveriesStopped says why the broker stopped handing the consumer
// messages.
func (c *inboundConsumer) whyDeliveriesStopped(ctx context.Context) error {
	if lost := c.link.lost(); lost != nil {
		return lost
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// The channel is open and the deliveries have stopped: the broker
	// cancelled the consumer. A new channel declares the queue again.
	c.link.dropChannel()
	return errConsumerCancelled
}

func (c *inboundConsumer) deadLetterQueue() string {
	return c.queue + inboundDeadLetterQueueSuffix
}
