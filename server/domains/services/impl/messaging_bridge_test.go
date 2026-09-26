package impl

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/rs/zerolog"
)

// The external-task bridge against a broker in memory: what it does with a
// task the broker did not take, and with the channel it was published on.

// taskBoard is the external-task service a bridge polls, in memory: the tasks
// of one topic, each offered until a fetch locks it, and offered again once it
// is handed back.
type taskBoard struct {
	mu         sync.Mutex
	tasks      map[uuid.UUID]*entities.ExternalTask
	waiting    []uuid.UUID
	locks      []int64 // the lock each fetch asked for, in milliseconds
	handedBack []uuid.UUID
	details    []string // why, for each hand-back
}

// newTaskBoard offers n tasks, and returns the board and their ids in the
// order they are offered.
func newTaskBoard(n int) (*taskBoard, []uuid.UUID) {
	board := &taskBoard{tasks: map[uuid.UUID]*entities.ExternalTask{}}
	ids := make([]uuid.UUID, 0, n)
	for range n {
		ids = append(ids, board.add())
	}
	return board, ids
}

// add offers one more task, and returns its id.
func (b *taskBoard) add() uuid.UUID {
	b.mu.Lock()
	defer b.mu.Unlock()
	task := &entities.ExternalTask{ID: uuid.New(), Topic: "reverse-charge", Retries: 3}
	b.tasks[task.ID] = task
	b.waiting = append(b.waiting, task.ID)
	return task.ID
}

func (b *taskBoard) FetchAndLock(_ context.Context, _, _ string, maxTasks int, lockDuration int64) ([]*entities.ExternalTask, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.locks = append(b.locks, lockDuration)
	n := min(maxTasks, len(b.waiting))
	fetched := make([]*entities.ExternalTask, 0, n)
	for _, id := range b.waiting[:n] {
		task := *b.tasks[id]
		fetched = append(fetched, &task)
	}
	b.waiting = b.waiting[n:]
	return fetched, nil
}

func (b *taskBoard) HandleFailure(ctx context.Context, taskID uuid.UUID, _, _, details string, _ int, _ int64) error {
	// The database refuses a context that has ended, and so does this.
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handedBack = append(b.handedBack, taskID)
	b.details = append(b.details, details)
	b.waiting = append(b.waiting, taskID)
	return nil
}

func (b *taskBoard) Complete(context.Context, uuid.UUID, string, map[string]any) error { return nil }

func (b *taskBoard) Create(context.Context, *entities.ExternalTask) error { return nil }

// returned says which tasks have been handed back, in order, and why.
func (b *taskBoard) returned() ([]uuid.UUID, []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.handedBack), slices.Clone(b.details)
}

// bridgeOn assembles a bridge over the fake broker and the board, as
// StartBridge does, logging to logs.
func bridgeOn(t *testing.T, broker *fakeBroker, board *taskBoard, logs *lockedBuffer, confirmTimeout time.Duration) *externalTaskBridge {
	t.Helper()
	svc := &messagingService{
		externalSvc:    board,
		dial:           broker.dial,
		confirmTimeout: confirmTimeout,
		sleep:          sleepWithContext,
	}
	logger := zerolog.New(logs)
	return svc.newBridge(logger.WithContext(t.Context()), uuid.New(), "reverse-charge", "amqp://broker.test/", "billing", "charges.reverse")
}

// pollWithin runs one round of the bridge, failing the test if the round has
// not finished within limit.
func pollWithin(t *testing.T, bridge *externalTaskBridge, limit time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		bridge.poll(t.Context())
	}()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("a round of the bridge was still running %s after it began", limit)
	}
}

func taskIDs(ids []uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

// A broker that takes a publish and never confirms it used to hold the bridge
// on that task for good: the task stayed locked until its lock ran out, and
// nothing else of the topic was forwarded until the server restarted.
func TestABridgeHandsBackATaskItsBrokerNeverConfirmsAndPublishesTheRestOnANewChannel(t *testing.T) {
	t.Parallel()
	var logs lockedBuffer
	broker := newFakeBroker()
	broker.answer(answerNever) // the first publish; every one after is taken
	board, ids := newTaskBoard(3)
	bridge := bridgeOn(t, broker, board, &logs, 50*time.Millisecond)

	pollWithin(t, bridge, 5*time.Second)

	// The unconfirmed task is handed back, and so are the two fetched with it:
	// they were not published on a channel that had missed a confirm.
	handedBack, details := board.returned()
	if !slices.Equal(handedBack, ids) {
		t.Fatalf("handed back %v after the broker did not confirm the first, want all three %v", handedBack, ids)
	}
	if !strings.Contains(details[0], "did not confirm") || !strings.Contains(details[1], "not published") {
		t.Errorf("the reasons given are %q", details)
	}
	if !broker.channel(0).isClosed() {
		t.Error("the channel that missed a confirm is still open")
	}

	// The next round publishes on a new channel of the same connection.
	pollWithin(t, bridge, 5*time.Second)
	if dials, channels := broker.counts(); dials != 1 || channels != 2 {
		t.Errorf("%d dials and %d channels, want the one connection and a second channel on it", dials, channels)
	}
	if delivered := broker.deliveredTaskIDs(); !slices.Equal(delivered, taskIDs(ids)) {
		t.Fatalf("the broker took %v, want the three tasks handed back %v", delivered, ids)
	}
}
