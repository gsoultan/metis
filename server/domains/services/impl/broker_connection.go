package impl

// brokerConnection is what the RabbitMQ bridge and consumer need from their
// connection to a broker.
//
// Their loops — reach the broker, publish or consume, hand back what was not
// taken, replace what can no longer be trusted — are the part that goes
// wrong, and with the AMQP library's types in their signatures the only way to
// run them was against a live broker, which no developer machine has. So they
// depend on this and on brokerChannel, which are only as wide as they need;
// amqpConnection and amqpChannel adapt the library to them, and the tests use
// a broker in memory.
type brokerConnection interface {
	// Channel opens a channel on the connection.
	Channel() (brokerChannel, error)
	closeWatcher
}
