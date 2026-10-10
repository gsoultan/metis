package impl

import (
	"errors"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
)

// TestAFetchIsBoundedOnEveryTransport: REST floored max_tasks and the lock,
// Connect and gRPC passed both through. A zero lock offered the task again at
// once, so two workers ran the same step; a huge count locked every task on a
// topic in one transaction.
func TestAFetchIsBoundedOnEveryTransport(t *testing.T) {
	day := entities.MaxExternalTaskLock.Milliseconds()

	for _, lock := range []int64{0, -1, day + 1} {
		if _, err := fetchLimits(5, lock); !errors.Is(err, apierr.ErrInvalidArgument) {
			t.Errorf("lock %d: err = %v, want invalid argument", lock, err)
		}
	}

	for _, c := range []struct{ asked, want int }{{0, 1}, {-3, 1}, {5, 5}, {1_000_000, maxFetchTasks}} {
		got, err := fetchLimits(c.asked, 60_000)
		if err != nil || got != c.want {
			t.Errorf("max_tasks %d: got %d, %v; want %d", c.asked, got, err, c.want)
		}
	}

	// The service itself refuses before it touches the repository.
	if _, err := (&externalTaskService{}).FetchAndLock(t.Context(), "topic", "w", 5, 0); !errors.Is(err, apierr.ErrInvalidArgument) {
		t.Errorf("FetchAndLock with a zero lock: err = %v, want invalid argument", err)
	}
}
