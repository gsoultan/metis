package app

import (
	"errors"
	"net"
	"strconv"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// rabbitMQBroker is where a bridge or consumer reaches its broker, once its
// connection has been read.
type rabbitMQBroker struct {
	// organization is whose work the bridge or consumer reads.
	organization uuid.UUID
	// url carries the broker's credentials. It is handed to the messaging
	// service and never written to a log.
	url string
	// address and vhost are what a log line can say about the broker without
	// saying how to log in to it.
	address string
	vhost   string
}

// rabbitMQBrokerAt reads a connection's URL the way the messaging service will
// dial it.
//
// Checked before anything is started with it, because the parser's error for a
// malformed URL quotes the URL, password and all, and the messaging service
// logs its dial errors on every retry. The reason given here says what is wrong
// without repeating the value.
func rabbitMQBrokerAt(url string) (rabbitMQBroker, error) {
	uri, err := amqp.ParseURI(url)
	if err != nil {
		return rabbitMQBroker{}, errors.New("its RabbitMQ URL is not a valid amqp:// or amqps:// URL; correct it on the Connectors page")
	}
	return rabbitMQBroker{
		url:     url,
		address: net.JoinHostPort(uri.Host, strconv.Itoa(uri.Port)),
		vhost:   uri.Vhost,
	}, nil
}
