package impl

import (
	"fmt"
)

// brokerLink is a bridge's hold on its broker: one connection, and one
// channel on it.
//
// It exists so that each can be replaced on its own. The bridge held the two
// as a pair of variables and replaced the pair only when the connection had
// gone, so a channel that could no longer be trusted was used for as long as
// the connection lived.
type brokerLink struct {
	url  string
	dial func(url string) (brokerConnection, error)

	conn brokerConnection
	ch   brokerChannel
}

// open returns an open channel, opening what it has to: a channel on the
// connection it has, or a connection first when it has none.
func (l *brokerLink) open() (brokerChannel, linkChange, error) {
	change := linkUnchanged
	if l.conn == nil || l.conn.IsClosed() {
		l.close()
		conn, err := l.dial(l.url)
		if err != nil {
			return nil, change, err
		}
		l.conn = conn
		change = linkReconnected
	}
	if l.ch != nil {
		return l.ch, change, nil
	}
	ch, err := l.conn.Channel()
	if err != nil {
		// A connection that will not open a channel is not one to keep.
		l.close()
		return nil, change, fmt.Errorf("could not open a channel: %w", err)
	}
	l.ch = ch
	if change == linkUnchanged {
		change = linkNewChannel
	}
	return l.ch, change, nil
}

// dropChannel closes the channel, so the next open replaces it on the same
// connection.
func (l *brokerLink) dropChannel() {
	if l.ch != nil {
		closeQuietly(l.ch, "AMQP channel")
		l.ch = nil
	}
}

// close lets go of the channel and the connection.
func (l *brokerLink) close() {
	l.dropChannel()
	if l.conn != nil {
		closeQuietly(l.conn, "AMQP connection")
		l.conn = nil
	}
}
