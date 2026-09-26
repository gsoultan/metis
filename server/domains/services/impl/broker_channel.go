package impl

// brokerChannel is a channel on a brokerConnection: what a confirmingPublisher
// publishes through, and something that can be closed and replaced.
type brokerChannel interface {
	confirmChannel
	Close() error
}
