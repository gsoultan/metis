package impl

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gsoultan/metis/server/repositories"
	"github.com/gsoultan/metis/tests/testutils"
)

func woken(s *jobService) bool {
	select {
	case <-s.wake:
		return true
	default:
		return false
	}
}

// The worker is woken by work that committed, once it has, and by nothing
// else: a rolled-back step queued no job to find, and a job due later is the
// tick's.
func TestTheJobWorkerIsWokenOnlyByDueWorkThatCommitted(t *testing.T) {
	repo := repositories.NewRepository(testutils.SetupTestDB(t))
	uow := repo.UnitOfWork()
	rolledBack := errors.New("the step failed")

	tests := []struct {
		name   string
		runAt  time.Time
		fail   bool
		wantUp bool
	}{
		{name: "due now and committed", runAt: time.Now(), wantUp: true},
		{name: "due now and rolled back", runAt: time.Now(), fail: true},
		{name: "due later and committed", runAt: time.Now().Add(time.Hour)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &jobService{repo: repo, wake: make(chan struct{}, 1)}
			err := uow.Do(t.Context(), func(ctx context.Context) error {
				s.wakeWhenDue(ctx, tt.runAt)
				if woken(s) {
					t.Fatal("the worker was woken before the job it would look for had committed")
				}
				if tt.fail {
					return rolledBack
				}
				return nil
			})
			if tt.fail != errors.Is(err, rolledBack) {
				t.Fatalf("transaction: %v", err)
			}
			if got := woken(s); got != tt.wantUp {
				t.Fatalf("woken = %v, want %v", got, tt.wantUp)
			}
		})
	}
}

// A burst of commits is one wake, not a queue of them, and waking a worker
// that is not running neither blocks nor leaks.
func TestWakingTheJobWorkerNeverBlocks(t *testing.T) {
	s := &jobService{wake: make(chan struct{}, 1)}
	for range 100 {
		s.wakeWorker()
	}
	if !woken(s) || woken(s) {
		t.Fatal("a burst of wakes should leave exactly one pending")
	}
}
