package impl

import (
	"context"
	"errors"
	"fmt"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// Publishing to a broker and knowing whether it arrived.
//
// An AMQP publish is fire-and-forget. Without publisher confirms the broker
// accepts the bytes and says nothing, so a rejected message or a connection
// that dropped mid-publish is indistinguishable from a delivered one. Without
// the mandatory flag a message the broker cannot route — no such exchange, or a
// routing key nothing is bound to — is discarded in silence and *still
// confirmed*, because the broker did take it; it simply had nowhere to put it.
//
// Either one reports a message as sent when it is gone. In an engine where a
// token has already advanced and the trail already says it was sent, there is
// nothing to investigate because nothing looks wrong.
//
// This is the one place that logic lives. It was written for the connector and
// then not used by the two paths in messaging.go, which is how those came to
// log "Forwarded external task to RabbitMQ" over a task that never left.

// How long a publish waits for the broker to confirm it.
//
// It used to wait for as long as the caller's context allowed, which for the
// external-task bridge and the RabbitMQ connector was forever. A broker that
// takes a publish and never answers — one out of memory or disk, or a
// connection half gone — held the bridge on that task until the server
// restarted, with the task locked and nothing else of its topic forwarded, and
// held a job worker's slot the same way. A confirm that has not come in this
// long is answered as a refusal. METIS_RABBITMQ_CONFIRM_TIMEOUT overrides it.
const (
	defaultConfirmTimeout = 10 * time.Second
	envConfirmTimeout     = "METIS_RABBITMQ_CONFIRM_TIMEOUT"
)

// errConfirmTimeout is a publish the broker did not confirm in time.
//
// Whether the message arrived is unknown, so it is answered as a refusal, and
// the channel is not used again: the broker's answer about the lost message
// may still come, and be read as its answer about the next one.
var errConfirmTimeout = errors.New("the broker did not confirm the message in time")

// rabbitMQConfirmTimeout reads METIS_RABBITMQ_CONFIRM_TIMEOUT, a Go duration
// such as "10s". Anything else, or nothing, is the default.
func rabbitMQConfirmTimeout() time.Duration {
	return envPositiveDuration(envConfirmTimeout, defaultConfirmTimeout)
}

// confirmingPublisher turns an AMQP channel into one where a publish that the
// broker did not take, or took and could not route, is an error.
//
// It holds the channel's return listener rather than registering one per
// publish: NotifyReturn appends a listener every time it is called, so doing
// that in a loop on a long-lived channel leaks one per message.
type confirmingPublisher struct {
	ch             confirmChannel
	returned       chan amqp.Return
	confirmTimeout time.Duration
}

// newConfirmingPublisher puts a channel into confirm mode and starts listening
// for returns. Call it once per channel. A confirmTimeout that is not above
// zero is the default: zero would fail every publish before the broker could
// answer.
func newConfirmingPublisher(ch confirmChannel, confirmTimeout time.Duration) (*confirmingPublisher, error) {
	if err := ch.Confirm(false); err != nil {
		return nil, fmt.Errorf("could not put the channel into confirm mode: %w", err)
	}
	if confirmTimeout <= 0 {
		confirmTimeout = defaultConfirmTimeout
	}
	return &confirmingPublisher{
		ch: ch,
		// Buffered so the library's dispatch goroutine is never blocked by a
		// return nobody has read yet.
		returned:       ch.NotifyReturn(make(chan amqp.Return, 1)),
		confirmTimeout: confirmTimeout,
	}, nil
}

// publish sends one message and waits for the broker to account for it.
//
// Returns nil only when the broker acknowledged the message *and* did not hand
// it back as unroutable.
func (p *confirmingPublisher) publish(ctx context.Context, exchange, routingKey string, msg amqp.Publishing) error {
	// A return left over from an earlier publish would otherwise be read as
	// this one's. Nothing should be here — the previous publish drains its own
	// — but a publish that failed before its confirm could leave one behind.
	select {
	case <-p.returned:
	default:
	}

	confirmation, err := p.ch.PublishWithDeferredConfirmWithContext(ctx,
		exchange,
		routingKey,
		true,  // mandatory: an unroutable message comes back rather than vanishing
		false, // immediate
		msg)
	if err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	acked, err := p.awaitConfirm(ctx, confirmation, exchange, routingKey)
	if err != nil {
		return err
	}
	if !acked {
		return fmt.Errorf("the broker refused the message published to exchange %q with routing key %q",
			exchange, routingKey)
	}

	// basic.return precedes basic.ack on the wire for an unroutable mandatory
	// message, and the library dispatches frames in order from one goroutine
	// into a buffered channel — so by the time the confirmation arrives, a
	// return for this message is already here if there is going to be one.
	select {
	case ret := <-p.returned:
		return fmt.Errorf(
			"the broker could not route the message to exchange %q with routing key %q and returned it (%d %s); nothing is bound to receive it",
			exchange, routingKey, ret.ReplyCode, ret.ReplyText)
	default:
	}

	return nil
}

// awaitConfirm waits for the broker's answer to one publish, for no longer
// than the publisher's confirm deadline or the caller's, whichever is sooner.
//
// Either deadline passing is errConfirmTimeout: the answer did not come in
// time, whoever stopped waiting for it. The caller stopping — a shutdown — is
// not, because nothing is wrong with the broker.
func (p *confirmingPublisher) awaitConfirm(ctx context.Context, confirmation publishConfirmation, exchange, routingKey string) (bool, error) {
	waitCtx, cancel := context.WithTimeoutCause(ctx, p.confirmTimeout, errConfirmTimeout)
	defer cancel()

	acked, err := confirmation.WaitContext(waitCtx)
	switch {
	case err == nil:
		return acked, nil
	case ctx.Err() == nil:
		return false, fmt.Errorf("%w: no answer within %s to the message published to exchange %q with routing key %q",
			errConfirmTimeout, p.confirmTimeout, exchange, routingKey)
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return false, fmt.Errorf("%w: %w", errConfirmTimeout, context.Cause(ctx))
	default:
		return false, fmt.Errorf("waiting for the broker to confirm the message: %w", err)
	}
}
