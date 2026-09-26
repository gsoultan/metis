package impl

import (
	amqp "github.com/rabbitmq/amqp091-go"
)

// amqpConnection is a brokerConnection through the AMQP library: the
// library's own connection, except that the channels it opens are adapted
// too.
type amqpConnection struct {
	*amqp.Connection
}

// dialAMQP connects to the broker at url.
func dialAMQP(url string) (brokerConnection, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, err
	}
	return amqpConnection{conn}, nil
}

func (c amqpConnection) Channel() (brokerChannel, error) {
	ch, err := c.Connection.Channel()
	if err != nil {
		return nil, err
	}
	return amqpChannel{ch}, nil
}
