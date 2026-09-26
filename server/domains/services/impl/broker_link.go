package impl

import (
	"errors"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

// brokerLink is a bridge's or consumer's hold on its broker: one connection,
// and one channel on it.
//
// It exists so that each can be replaced on its own, and so that one the
// broker closes is noticed. The bridge held the two as a pair of variables
// and replaced them only when the connection had gone; a channel the broker
// closed — it does, on a publish to an exchange that does not exist — was
// used, and refused every publish, for as long as the connection lived.
//
// The broker says it has closed either one on a notification it sends once;
// the reason is kept, so it can be asked for more than once.
type brokerLink struct {
	url  string
	dial func(url string) (brokerConnection, error)

	conn        brokerConnection
	connClosing chan *amqp.Error
	connWhy     error
	ch          brokerChannel
	chClosing   chan *amqp.Error
	chWhy       error
}

// open returns an open channel, opening what it has to: a new channel when
// it has none or the broker closed the one it had, and a new connection first
// when the broker closed that too.
func (l *brokerLink) open() (brokerChannel, linkChange, error) {
	l.discardWhatClosed()
	change := linkUnchanged
	if l.conn == nil {
		if err := l.connect(); err != nil {
			return nil, change, err
		}
		change = linkReconnected
	}
	if l.ch != nil {
		return l.ch, change, nil
	}
	if err := l.openChannel(); err != nil {
		return nil, change, err
	}
	if change == linkUnchanged {
		change = linkNewChannel
	}
	return l.ch, change, nil
}

// lost reports why the broker closed the connection or the channel, or nil
// while both are open. The connection's reason comes first: closing it closes
// its channels too.
func (l *brokerLink) lost() error {
	l.watch()
	if l.connWhy != nil {
		return l.connWhy
	}
	return l.chWhy
}

// connectionLost reports why the broker closed the connection, or nil while
// it is up.
func (l *brokerLink) connectionLost() error {
	l.watch()
	return l.connWhy
}

// dropChannel closes the channel, so the next open replaces it on the same
// connection.
func (l *brokerLink) dropChannel() {
	if l.ch != nil {
		closeQuietly(l.ch, "AMQP channel")
	}
	l.ch, l.chClosing, l.chWhy = nil, nil, nil
}

// close lets go of the channel and the connection.
func (l *brokerLink) close() {
	l.dropChannel()
	if l.conn != nil {
		closeQuietly(l.conn, "AMQP connection")
	}
	l.conn, l.connClosing, l.connWhy = nil, nil, nil
}

func (l *brokerLink) connect() error {
	conn, err := l.dial(l.url)
	if err != nil {
		return err
	}
	l.conn = conn
	l.connClosing = conn.NotifyClose(make(chan *amqp.Error, 1))
	return nil
}

func (l *brokerLink) openChannel() error {
	ch, err := l.conn.Channel()
	if err != nil {
		// A connection that will not open a channel is not one to keep.
		l.close()
		return fmt.Errorf("could not open a channel: %w", err)
	}
	l.ch = ch
	l.chClosing = ch.NotifyClose(make(chan *amqp.Error, 1))
	return nil
}

// watch reads, without waiting, whether the broker has closed the connection
// or the channel, and keeps why.
func (l *brokerLink) watch() {
	if l.conn != nil && l.connWhy == nil {
		l.connWhy = closedBecause(l.connClosing, "the broker closed the connection")
	}
	if l.ch != nil && l.chWhy == nil {
		l.chWhy = closedBecause(l.chClosing, "the broker closed the channel")
	}
}

// discardWhatClosed lets go of what the broker has closed, so that open
// replaces it: the channel alone while the connection is up.
func (l *brokerLink) discardWhatClosed() {
	l.watch()
	switch {
	case l.connWhy != nil:
		l.close()
	case l.chWhy != nil:
		l.dropChannel()
	}
}

// closedBecause reads a close notification without waiting: nil while
// nothing has closed, and otherwise what closed, with the broker's reason
// when it gave one.
func closedBecause(notification chan *amqp.Error, what string) error {
	select {
	case reason, received := <-notification:
		if received && reason != nil {
			return fmt.Errorf("%s: %w", what, reason)
		}
		return errors.New(what)
	default:
		return nil
	}
}
