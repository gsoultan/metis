package task_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

// A notification is sent by the engine while it creates, claims or assigns a
// task — inside that transaction. Delivering it there made a webhook or an SMTP
// conversation hold the transaction's connection and the task's row locks for
// as long as somebody else's server took to answer, and delivered it even when
// the transaction then rolled back.

type recordingChannel struct {
	delivered chan string
}

func (c *recordingChannel) Name() string { return "recording" }

func (c *recordingChannel) Deliver(_ context.Context, n entities.Notification) error {
	c.delivered <- n.Title
	return nil
}

func deliveringFixture(t *testing.T) (repositories.Repository, context.Context, *recordingChannel, func(context.Context, string) error) {
	t.Helper()
	repo := repositories.NewRepository(testutils.SetupTestConn(t))
	ctx, _ := testutils.ScopedContext(t, repo)
	channel := &recordingChannel{delivered: make(chan string, 8)}
	// One worker, so deliveries arrive in the order they were queued.
	svc := serviceimpl.NewDeliveringNotificationService(
		serviceimpl.NewNotificationService(repo.Notification()),
		serviceimpl.DeliverAfterCommit(t.Context(), repo.UnitOfWork().AfterCommit, 1, 8),
		channel,
	)
	send := func(ctx context.Context, title string) error {
		return svc.Send(ctx, entities.Notification{
			User:  &entities.User{Username: "ollie"},
			Type:  entities.NotificationTaskAssignment,
			Title: title,
		})
	}
	return repo, ctx, channel, send
}

func nextDelivery(t *testing.T, channel *recordingChannel) string {
	t.Helper()
	select {
	case title := <-channel.delivered:
		return title
	case <-time.After(10 * time.Second):
		t.Fatal("nothing was delivered")
		return ""
	}
}

func TestANotificationIsDeliveredOnlyOnceItsTransactionHasCommitted(t *testing.T) {
	repo, ctx, channel, send := deliveringFixture(t)

	err := repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		if err := send(txCtx, "A task is waiting for you"); err != nil {
			return err
		}
		select {
		case title := <-channel.delivered:
			t.Errorf("%q was delivered while the transaction that stored it was still open", title)
		default:
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the transaction: %v", err)
	}

	if got := nextDelivery(t, channel); got != "A task is waiting for you" {
		t.Fatalf("delivered %q", got)
	}
}

func TestANotificationAboutWorkThatRolledBackIsNeverDelivered(t *testing.T) {
	repo, ctx, channel, send := deliveringFixture(t)

	undone := errors.New("the task this was about was never created")
	err := repo.UnitOfWork().Do(ctx, func(txCtx context.Context) error {
		if err := send(txCtx, "About work that was undone"); err != nil {
			return err
		}
		return undone
	})
	if !errors.Is(err, undone) {
		t.Fatalf("the transaction should have rolled back, got %v", err)
	}

	// Deliveries arrive in order, so if the rolled-back one had been queued it
	// would arrive before this one.
	if err := send(ctx, "About work that happened"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got := nextDelivery(t, channel); got != "About work that happened" {
		t.Fatalf("delivered %q, which a rollback undid", got)
	}
}
