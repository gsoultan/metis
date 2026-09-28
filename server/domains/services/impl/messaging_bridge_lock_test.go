package impl

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/rs/zerolog"
)

// A worker consuming from the queue holds nothing but the message. Extending
// the bridge's lock takes the task's id and the worker id the lock is held
// under, and knowing when to extend takes the lock's expiry, so the message
// carries all three as the fetch locked them. internal/app's broker test does
// the extension itself, against a real broker, in CI.
func TestABridgedMessageCarriesWhatItsWorkerNeedsToExtendTheLock(t *testing.T) {
	t.Parallel()
	broker := newFakeBroker()
	tasks := &lockingTaskService{task: entities.ExternalTask{ID: uuid.New(), Topic: "reverse-charge", Retries: 3}}
	svc := &messagingService{externalSvc: tasks, dial: broker.dial, confirmTimeout: time.Second, sleep: sleepWithContext}
	logger := zerolog.Nop()
	bridge := svc.newBridge(logger.WithContext(t.Context()), uuid.New(), "reverse-charge", "amqp://broker.test/", "billing", "charges.reverse", 10*time.Minute)

	pollWithin(t, bridge, 5*time.Second)

	broker.mu.Lock()
	delivered := append([]byte(nil), firstBody(broker)...)
	broker.mu.Unlock()
	var message struct {
		ID             string    `json:"id"`
		WorkerID       string    `json:"worker_id"`
		LockExpiration time.Time `json:"lock_expiration"`
	}
	if err := json.Unmarshal(delivered, &message); err != nil {
		t.Fatalf("the bridge published %q: %v", delivered, err)
	}
	if message.ID != tasks.task.ID.String() || message.WorkerID != workerID || !message.LockExpiration.Equal(tasks.until) {
		t.Fatalf("the message carries id %q, worker_id %q and lock_expiration %v; want %s, %q and %v",
			message.ID, message.WorkerID, message.LockExpiration, tasks.task.ID, workerID, tasks.until)
	}
}

// firstBody is the body of the first message the broker took, or nothing.
func firstBody(b *fakeBroker) []byte {
	if len(b.delivered) == 0 {
		return nil
	}
	return b.delivered[0].Body
}

// lockingTaskService offers one task, and locks it as the repository locks a
// fetched task: to the worker that fetched it, for as long as it asked.
type lockingTaskService struct {
	externalTaskStub
	task  entities.ExternalTask
	until time.Time
}

func (s *lockingTaskService) FetchAndLock(_ context.Context, _, worker string, _ int, lockDuration int64) ([]*entities.ExternalTask, error) {
	s.until = time.Now().Add(time.Duration(lockDuration) * time.Millisecond).UTC()
	locked := s.task
	locked.WorkerID = worker
	locked.LockExpiration = &s.until
	return []*entities.ExternalTask{&locked}, nil
}
