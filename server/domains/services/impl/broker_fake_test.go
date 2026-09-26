package impl

import (
	"context"
	"sync"

	amqp "github.com/rabbitmq/amqp091-go"
)

// A RabbitMQ broker in memory, for the loops that reach a broker, replace what
// cannot be trusted and hand back what was not taken. Every dial, channel,
// declaration and publish is counted, the test decides how each publish is
// answered, and it can close a channel or a connection, or cancel a consumer,
// as a broker does.

// publishAnswer is how the fake broker answers one publish.
type publishAnswer int

const (
	// answerAck takes the message.
	answerAck publishAnswer = iota
	// answerNack refuses it.
	answerNack
	// answerNever takes it and never says so: no ack, no nack.
	answerNever
	// answerCloseChannel closes the channel, as a broker does for a publish to
	// an exchange that does not exist.
	answerCloseChannel
)

// missingExchange is the broker's reason for closing a channel that published
// to an exchange that does not exist.
var missingExchange = &amqp.Error{Code: amqp.NotFound, Reason: "NOT_FOUND - no exchange 'billing' in vhost '/'", Server: true}

type fakeBroker struct {
	mu        sync.Mutex
	dials     int
	refusing  bool    // every dial fails while set, as with a broker that is down
	dialErrs  []error // one per dial, in order; nil or exhausted means the dial succeeds
	conns     []*fakeConnection
	channels  []*fakeChannel
	declared  []string // every queue declared, in order
	answers   []publishAnswer
	delivered []amqp.Publishing
	settled   []string // "ack" or "requeue" or "drop", one per settled delivery
}

func newFakeBroker() *fakeBroker { return &fakeBroker{} }

// answer queues how the next publishes are answered; once they are used up,
// every publish is taken.
func (b *fakeBroker) answer(answers ...publishAnswer) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.answers = append(b.answers, answers...)
}

// failDials makes the next dials fail, one error each.
func (b *fakeBroker) failDials(errs ...error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dialErrs = append(b.dialErrs, errs...)
}

// refuse makes every dial fail while on is true.
func (b *fakeBroker) refuse(on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refusing = on
}

func (b *fakeBroker) dial(string) (brokerConnection, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dials++
	if b.refusing {
		return nil, errConnectionRefused
	}
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

// declaredQueues returns every queue declared, in order.
func (b *fakeBroker) declaredQueues() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.declared...)
}

// settlements returns how each delivery was settled, in order.
func (b *fakeBroker) settlements() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.settled...)
}

// pendingPublishes counts the publishes the broker has not answered.
func (b *fakeBroker) pendingPublishes() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	pending := 0
	for _, ch := range b.channels {
		pending += len(ch.pending)
	}
	return pending
}

// channel returns the nth channel opened, from zero.
func (b *fakeBroker) channel(n int) *fakeChannel {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.channels[n]
}

// dropConnection closes the newest connection as a broker does, with reason,
// and every channel on it with it.
func (b *fakeBroker) dropConnection(reason *amqp.Error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.conns[len(b.conns)-1].shut(reason)
}

func (b *fakeBroker) nextAnswer() publishAnswer {
	if len(b.answers) == 0 {
		return answerAck
	}
	answer := b.answers[0]
	b.answers = b.answers[1:]
	return answer
}

// notifyClosed tells close listeners why, when there is a reason, and closes
// them, as the library does. The broker's lock is held.
func notifyClosed(listeners []chan *amqp.Error, reason *amqp.Error) {
	for _, listener := range listeners {
		if reason != nil {
			select {
			case listener <- reason:
			default:
			}
		}
		close(listener)
	}
}

type fakeConnection struct {
	broker   *fakeBroker
	closed   bool
	closes   []chan *amqp.Error
	channels []*fakeChannel
}

func (c *fakeConnection) Channel() (brokerChannel, error) {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()
	if c.closed {
		return nil, amqp.ErrClosed
	}
	ch := &fakeChannel{broker: c.broker, gone: make(chan struct{})}
	c.channels = append(c.channels, ch)
	c.broker.channels = append(c.broker.channels, ch)
	return ch, nil
}

func (c *fakeConnection) NotifyClose(receiver chan *amqp.Error) chan *amqp.Error {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()
	if c.closed {
		close(receiver)
		return receiver
	}
	c.closes = append(c.closes, receiver)
	return receiver
}

func (c *fakeConnection) Close() error {
	c.broker.mu.Lock()
	defer c.broker.mu.Unlock()
	if c.closed {
		return amqp.ErrClosed
	}
	c.shut(nil)
	return nil
}

// shut closes the connection and its channels, with the broker's reason when
// it is the broker closing it. The broker's lock is held.
func (c *fakeConnection) shut(reason *amqp.Error) {
	if c.closed {
		return
	}
	c.closed = true
	notifyClosed(c.closes, reason)
	c.closes = nil
	for _, ch := range c.channels {
		ch.shut(reason)
	}
}

type fakeChannel struct {
	broker     *fakeBroker
	confirming bool
	closed     bool
	gone       chan struct{} // closed with the channel
	closes     []chan *amqp.Error
	// pending are the publishes answered answerNever. The library nacks
	// every outstanding one when a channel closes, and so does this.
	pending    []*fakeConfirmation
	deliveries chan amqp.Delivery // the consumer's, while it has one
	tag        uint64
}

// isClosed reports whether the channel has been closed.
func (ch *fakeChannel) isClosed() bool {
	ch.broker.mu.Lock()
	defer ch.broker.mu.Unlock()
	return ch.closed
}

// closeWith closes the channel as a broker does, with reason.
func (ch *fakeChannel) closeWith(reason *amqp.Error) {
	ch.broker.mu.Lock()
	defer ch.broker.mu.Unlock()
	ch.shut(reason)
}

// cancelConsumer stops the channel's consumer as a broker does when its queue
// is deleted: the deliveries end and the channel stays open.
func (ch *fakeChannel) cancelConsumer() {
	ch.broker.mu.Lock()
	defer ch.broker.mu.Unlock()
	ch.endConsumer()
}

// deliver hands the channel's consumer a message, and reports false when the
// channel has none.
func (ch *fakeChannel) deliver(body string) bool {
	ch.broker.mu.Lock()
	defer ch.broker.mu.Unlock()
	if ch.deliveries == nil {
		return false
	}
	ch.tag++
	ch.deliveries <- amqp.Delivery{Acknowledger: fakeAcknowledger{broker: ch.broker}, DeliveryTag: ch.tag, Body: []byte(body)}
	return true
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
	return receiver
}

func (ch *fakeChannel) NotifyClose(receiver chan *amqp.Error) chan *amqp.Error {
	ch.broker.mu.Lock()
	defer ch.broker.mu.Unlock()
	if ch.closed {
		close(receiver)
		return receiver
	}
	ch.closes = append(ch.closes, receiver)
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
	case answerCloseChannel:
		confirmation := &fakeConfirmation{done: make(chan struct{})}
		ch.pending = append(ch.pending, confirmation)
		ch.shut(missingExchange)
		return confirmation, nil
	default:
		ch.broker.delivered = append(ch.broker.delivered, msg)
		return answeredConfirmation(true), nil
	}
}

func (ch *fakeChannel) QueueDeclare(name string, _, _, _, _ bool, _ amqp.Table) (amqp.Queue, error) {
	ch.broker.mu.Lock()
	defer ch.broker.mu.Unlock()
	if ch.closed {
		return amqp.Queue{}, amqp.ErrClosed
	}
	ch.broker.declared = append(ch.broker.declared, name)
	return amqp.Queue{Name: name}, nil
}

func (ch *fakeChannel) Qos(int, int, bool) error {
	ch.broker.mu.Lock()
	defer ch.broker.mu.Unlock()
	if ch.closed {
		return amqp.ErrClosed
	}
	return nil
}

// ConsumeWithContext starts the channel's consumer. Its deliveries end when
// the channel closes, when the broker cancels it, or when ctx ends, as the
// library's do.
func (ch *fakeChannel) ConsumeWithContext(ctx context.Context, _, _ string, _, _, _, _ bool, _ amqp.Table) (<-chan amqp.Delivery, error) {
	ch.broker.mu.Lock()
	defer ch.broker.mu.Unlock()
	if ch.closed {
		return nil, amqp.ErrClosed
	}
	deliveries := make(chan amqp.Delivery, 16)
	ch.deliveries = deliveries
	go func() {
		select {
		case <-ctx.Done():
			ch.cancelConsumer()
		case <-ch.gone:
		}
	}()
	return deliveries, nil
}

func (ch *fakeChannel) Close() error {
	ch.broker.mu.Lock()
	defer ch.broker.mu.Unlock()
	ch.shut(nil)
	return nil
}

// shut closes the channel as the library does: outstanding publishes are
// nacked, close listeners are told why when the broker closed it, and the
// consumer's deliveries end. The broker's lock is held.
func (ch *fakeChannel) shut(reason *amqp.Error) {
	if ch.closed {
		return
	}
	ch.closed = true
	close(ch.gone)
	notifyClosed(ch.closes, reason)
	ch.closes = nil
	for _, confirmation := range ch.pending {
		close(confirmation.done)
	}
	ch.pending = nil
	ch.endConsumer()
}

// endConsumer closes the consumer's deliveries. The broker's lock is held.
func (ch *fakeChannel) endConsumer() {
	if ch.deliveries != nil {
		close(ch.deliveries)
		ch.deliveries = nil
	}
}

// fakeAcknowledger records how the consumer settled each delivery.
type fakeAcknowledger struct {
	broker *fakeBroker
}

func (a fakeAcknowledger) settle(outcome string) error {
	a.broker.mu.Lock()
	defer a.broker.mu.Unlock()
	a.broker.settled = append(a.broker.settled, outcome)
	return nil
}

func (a fakeAcknowledger) Ack(uint64, bool) error { return a.settle("ack") }

func (a fakeAcknowledger) Nack(_ uint64, _, requeue bool) error {
	if requeue {
		return a.settle("requeue")
	}
	return a.settle("drop")
}

func (a fakeAcknowledger) Reject(_ uint64, requeue bool) error { return a.Nack(0, false, requeue) }

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
