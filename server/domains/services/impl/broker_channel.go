package impl

// brokerChannel is a channel on a brokerConnection: what a confirmingPublisher
// publishes through, what the inbound consumer reads a queue with, and
// something that says when the broker has closed it.
type brokerChannel interface {
	confirmChannel
	queueChannel
	closeWatcher
}
