package impl

import (
	"context"
	"errors"

	amqp "github.com/rabbitmq/amqp091-go"
)

// errNotInConfirmMode is a publish on a channel that was never put into
// confirm mode, where nothing would ever say whether the broker took it.
var errNotInConfirmMode = errors.New("the channel is not in confirm mode, so nothing would say whether the broker took the message")

// amqpChannel is a brokerChannel through the AMQP library.
//
// Its one method of its own hands the library's deferred confirmation back as
// the interface the publisher waits on. It refuses a nil one, which the
// library returns for a channel not in confirm mode, and which would
// otherwise be dereferenced the first time anybody waited on it.
type amqpChannel struct {
	*amqp.Channel
}

func (c amqpChannel) PublishWithDeferredConfirmWithContext(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) (publishConfirmation, error) {
	confirmation, err := c.Channel.PublishWithDeferredConfirmWithContext(ctx, exchange, key, mandatory, immediate, msg)
	if err != nil {
		return nil, err
	}
	if confirmation == nil {
		return nil, errNotInConfirmMode
	}
	return confirmation, nil
}
