package impl

// brokerConnection is what the RabbitMQ bridge needs from its connection to a
// broker.
//
// The bridge's loop — reach the broker, publish, hand back what was not taken,
// replace what can no longer be trusted — is the part that goes wrong, and
// with the AMQP library's types in its signature the only way to run it was
// against a live broker, which no developer machine has. So the loop depends
// on this and on brokerChannel, which are only as wide as it needs;
// amqpConnection and amqpChannel adapt the library to them, and the tests use
// a broker in memory.
type brokerConnection interface {
	// Channel opens a channel on the connection.
	Channel() (brokerChannel, error)
	// IsClosed reports whether the connection has gone.
	IsClosed() bool
	Close() error
}
