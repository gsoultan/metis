package impl

import (
	"context"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

// A RabbitMQ broker in memory, for the loops that reach a broker, replace what
// cannot be trusted and hand back what was not taken. Every dial, channel and
// publish is counted, and the test decides how each publish is answered.

// publishAnswer is how the fake broker answers one publish.
type publishAnswer int

const (
	// answerAck takes the message.
	answerAck publishAnswer = iota
	// answerNack refuses it.
	answerNack
	// answerNever takes it and never says so: no ack, no nack.
	answerNever
)

type fakeBroker struct {
	mu        sync.Mutex
	dials     int
	dialErrs  []error // one per dial, in order; nil or exhausted means the dial succeeds
	conns     []*fakeConnection
	channels  []*fakeChannel
	answers   []publishAnswer // one per publish, in order; exhausted means ack
	delivered []amqp.Publishing
}

func newFakeBroker() *fakeBroker { return &fakeBroker{} }

// answer queues how the next publishes are answered.
func (b *fakeBroker) answer(answers ...publishAnswer) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.answers = append(b.answers, answers...)
}

func (b *fakeBroker) dial(string) (brokerConnection, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dials++
	if len(b.dialErrs) > 0 {
		err := b.dialErrs[0]
		b.dialErrs = b.dialErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	conn := &fakeConnection{broker: b}
	b.conns = append(b.conns, conn)
	return conn, nil
}

// counts returns how many dials and channels there have been.
func (b *fakeBroker) counts() (dials, channels int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dials, len(b.channels)
}

// deliveredTaskIDs returns the task_id header of every message the broker
// took, in order.
func (b *fakeBroker) deliveredTaskIDs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var ids []string
	for _, message := range b.delivered {
		id, _ := message.Headers["task_id"].(string)
		ids = append(ids, id)
	}
	return ids
}

// channel returns the nth channel opened, from zero.
func (b *fakeBroker) channel(n int) *fakeChannel {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.channels[n]
}

func (b *fakeBroker) nextAnswer() publishAnswer {
	if len(b.answers) == 0 {
		return answerAck
	}
	answer := b.answers[0]
	b.answers = b.answers[1:]
	return answer
}

type fakeConnection struct {
	broker *fakeBroker
	closed bool
}

func (c *fakeConnection) Channel() (brokerChannel, error) {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()
	if c.closed {
		return nil, amqp.ErrClosed
	}
	ch := &fakeChannel{broker: c.broker, conn: c}
	c.broker.channels = append(c.broker.channels, ch)
	return ch, nil
}

func (c *fakeConnection) IsClosed() bool {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()
	return c.closed
}

func (c *fakeConnection) Close() error {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()
	c.closed = true
	return nil
}

type fakeChannel struct {
	broker     *fakeBroker
	conn       *fakeConnection
	confirming bool
	closed     bool
	returns    chan amqp.Return
	// pending are the publishes answered answerNever. The library nacks
	// every outstanding one when a channel closes, and so does this.
	pending []*fakeConfirmation
}

// isClosed reports whether the channel has been closed.
func (ch *fakeChannel) isClosed() bool {
	ch.broker.mu.Lock()
	defer ch.broker.mu.Unlock()
	return ch.closed
}

func (ch *fakeChannel) Confirm(bool) error {
	ch.broker.mu.Lock()
	defer ch.broker.mu.Unlock()
	if ch.closed {
		return amqp.ErrClosed
	}
	ch.confirming = true
	return nil
}

func (ch *fakeChannel) NotifyReturn(receiver chan amqp.Return) chan amqp.Return {
	ch.broker.mu.Lock()
	defer ch.broker.mu.Unlock()
	ch.returns = receiver
	return receiver
}

func (ch *fakeChannel) PublishWithDeferredConfirmWithContext(ctx context.Context, _, _ string, _, _ bool, msg amqp.Publishing) (publishConfirmation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ch.broker.mu.Lock()
	defer ch.broker.mu.Unlock()
	if ch.closed {
		return nil, amqp.ErrClosed
	}
	if !ch.confirming {
		return nil, errNotInConfirmMode
	}
	switch ch.broker.nextAnswer() {
	case answerNack:
		return answeredConfirmation(false), nil
	case answerNever:
		confirmation := &fakeConfirmation{done: make(chan struct{})}
		ch.pending = append(ch.pending, confirmation)
		return confirmation, nil
	default:
		ch.broker.delivered = append(ch.broker.delivered, msg)
		return answeredConfirmation(true), nil
	}
}

func (ch *fakeChannel) Close() error {
	ch.broker.mu.Lock()
	defer ch.broker.mu.Unlock()
	ch.shut()
	return nil
}

// shut closes the channel as the library does, nacking what is outstanding.
// The broker's lock is held.
func (ch *fakeChannel) shut() {
	if ch.closed {
		return
	}
	ch.closed = true
	for _, confirmation := range ch.pending {
		close(confirmation.done)
	}
	ch.pending = nil
}

// fakeConfirmation is the answer to one publish: acked once done is closed.
type fakeConfirmation struct {
	done  chan struct{}
	acked bool
}

func answeredConfirmation(acked bool) *fakeConfirmation {
	confirmation := &fakeConfirmation{done: make(chan struct{}), acked: acked}
	close(confirmation.done)
	return confirmation
}

func (c *fakeConfirmation) WaitContext(ctx context.Context) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-c.done:
		return c.acked, nil
	}
}
