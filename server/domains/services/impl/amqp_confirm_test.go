package impl

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// A publish waits for the broker to say it took the message, and not for
// ever. It waited for as long as the caller's context allowed, which for the
// bridge and the connector was forever: a broker that took a publish and never
// answered held the bridge, and the task it had locked, until a restart.
func TestAPublishIsAnsweredWithinItsConfirmDeadline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		answer         publishAnswer
		callerDeadline time.Duration // zero: the caller sets none
		callerCancels  bool
		wantMissed     bool   // an errConfirmTimeout
		wantText       string // what the error says, when there is one
	}{
		{name: "taken", answer: answerAck},
		{name: "refused", answer: answerNack, wantText: "the broker refused the message"},
		{
			name:       "never answered",
			answer:     answerNever,
			wantMissed: true,
			wantText:   `no answer within 50ms to the message published to exchange "billing"`,
		},
		{
			// The dead-letter publish runs under a deadline of its own that is
			// shorter than the confirm's. The confirm still did not come.
			name:           "never answered within the caller's shorter deadline",
			answer:         answerNever,
			callerDeadline: 20 * time.Millisecond,
			wantMissed:     true,
			wantText:       "context deadline exceeded",
		},
		{
			// Stopping is not the broker failing to answer, and must not read
			// as though it were.
			name:          "the caller stops waiting",
			answer:        answerNever,
			callerCancels: true,
			wantText:      "context canceled",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			broker := newFakeBroker()
			broker.answer(tc.answer)
			publisher := confirmingPublisherOn(t, broker, 50*time.Millisecond)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.callerDeadline > 0 {
				ctx, cancel = context.WithTimeout(ctx, tc.callerDeadline)
				defer cancel()
			}
			if tc.callerCancels {
				time.AfterFunc(20*time.Millisecond, cancel)
			}

			err := publishWithin(t, publisher, ctx, 5*time.Second)
			if tc.wantText == "" {
				if err != nil {
					t.Fatalf("a publish the broker took failed: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantText) {
				t.Fatalf("the publish returned %v, want an error saying %q", err, tc.wantText)
			}
			if missed := errors.Is(err, errConfirmTimeout); missed != tc.wantMissed {
				t.Fatalf("errors.Is(err, errConfirmTimeout) = %v, want %v: %v", missed, tc.wantMissed, err)
			}
		})
	}
}

// A confirm deadline that is not above zero would fail every publish before
// the broker could answer; it is the default instead.
func TestAConfirmDeadlineOfZeroIsTheDefault(t *testing.T) {
	t.Parallel()
	publisher := confirmingPublisherOn(t, newFakeBroker(), 0)
	if publisher.confirmTimeout != defaultConfirmTimeout {
		t.Fatalf("a publisher given no deadline waits %v, want the default %v", publisher.confirmTimeout, defaultConfirmTimeout)
	}
}

// confirmingPublisherOn opens a channel on the fake broker and puts a
// publisher on it.
func confirmingPublisherOn(t *testing.T, broker *fakeBroker, confirmTimeout time.Duration) *confirmingPublisher {
	t.Helper()
	conn, err := broker.dial("amqp://broker.test/")
	if err != nil {
		t.Fatalf("dial the fake broker: %v", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		t.Fatalf("open a channel on the fake broker: %v", err)
	}
	publisher, err := newConfirmingPublisher(ch, confirmTimeout)
	if err != nil {
		t.Fatalf("put the channel into confirm mode: %v", err)
	}
	return publisher
}

// publishWithin publishes one message, failing the test if the publish has not
// returned within limit.
func publishWithin(t *testing.T, publisher *confirmingPublisher, ctx context.Context, limit time.Duration) error {
	t.Helper()
	published := make(chan error, 1)
	go func() {
		published <- publisher.publish(ctx, "billing", "charges.reverse", amqp.Publishing{Body: []byte(`{}`)})
	}()
	select {
	case err := <-published:
		return err
	case <-time.After(limit):
		t.Fatalf("%s after publishing, the publish is still waiting for a confirm the broker never sent", limit)
		return nil
	}
}
