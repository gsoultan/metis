package impl

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
)

// sweptStore is a repository with a unit of work that commits and a store of
// requests that only bounds a wait, and counts how often it was asked to.
type sweptStore struct {
	repositories.Repository
	bounds *boundsCounted
}

func (s sweptStore) UnitOfWork() repocontracts.UnitOfWork { return &uowThatCommits{} }

func (s sweptStore) DeviationRequest() repocontracts.DeviationRequestRepository { return s.bounds }

type boundsCounted struct {
	repocontracts.DeviationRequestRepository
	waits []time.Duration
}

func (b *boundsCounted) BoundLockWait(_ context.Context, wait time.Duration) error {
	b.waits = append(b.waits, wait)
	return nil
}

// overdueInOrder is so many requests, each a minute later overdue than the
// one before, and a read of them as the repository reads: in order, strictly
// after a cursor, up to a limit. It records every cursor it was read from.
func overdueInOrder(count int) (requests []entities.DeviationRequest, read sweepRead, cursors *[]repocontracts.SweepCursor) {
	base := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	for i := range count {
		requests = append(requests, entities.DeviationRequest{ID: uuid.Must(uuid.NewV7()), ExpiresAt: base.Add(time.Duration(i) * time.Minute)})
	}
	cursors = &[]repocontracts.SweepCursor{}
	read = func(_ context.Context, after repocontracts.SweepCursor, limit int) ([]entities.DeviationRequest, error) {
		*cursors = append(*cursors, after)
		var page []entities.DeviationRequest
		for _, request := range requests {
			if !after.AtStart() && !request.ExpiresAt.After(after.ExpiresAt) {
				continue
			}
			if page = append(page, request); len(page) == limit {
				break
			}
		}
		return page, nil
	}
	return requests, read, cursors
}

// A pass reads every request once, in order, each from the cursor the one
// before it left — whatever became of that one. A request that could not be
// closed is counted and passed; one somebody else decided first is passed and
// counted as neither; the rest are closed. Each closing bounds its own wait
// for a lock first.
func TestAPassReadsEachRequestOnceAndGoesOnPastWhatItCannotClose(t *testing.T) {
	requests, read, cursors := overdueInOrder(7)
	fails := map[uuid.UUID]bool{requests[0].ID: true, requests[1].ID: true, requests[4].ID: true}
	var met []uuid.UUID
	bounds := &boundsCounted{}
	service := &deviationRequestService{repo: sweptStore{bounds: bounds}, sweepLockWait: 2 * time.Second, sweepBudget: 100}
	tally := service.sweep(context.Background(), read, func(_ context.Context, request entities.DeviationRequest) error {
		met = append(met, request.ID)
		switch {
		case fails[request.ID]:
			return errors.New("its ledger row no longer waits")
		case request.ID == requests[2].ID:
			return repocontracts.ErrDeviationRequestDecided
		}
		return nil
	})
	want := make([]uuid.UUID, 0, len(requests))
	for _, request := range requests {
		want = append(want, request.ID)
	}
	if !slices.Equal(met, want) {
		t.Fatalf("the pass met %v; want each of the seven once, in order", met)
	}
	if tally.closed != 3 || tally.failed != 3 || tally.spent || tally.stopped != nil {
		t.Fatalf("the pass tallied %+v; want 3 closed, 3 not, 1 decided by somebody else and counted as neither", tally)
	}
	if tally.first == nil || !strings.Contains(tally.first.Error(), requests[0].ID.String()) || !strings.Contains(tally.first.Error(), "its ledger row no longer waits") {
		t.Fatalf("the first failure reads %v; want the oldest request and its cause", tally.first)
	}
	// Eight reads: one from the start, one after each request, the last
	// answering nothing.
	if len(*cursors) != 8 || !(*cursors)[0].AtStart() {
		t.Fatalf("the pass read from %d cursors, the first %+v; want eight, from the start", len(*cursors), (*cursors)[0])
	}
	for i, request := range requests {
		if got := (*cursors)[i+1]; got != repocontracts.SweepCursorAt(request) {
			t.Fatalf("read %d was made after %+v; want after request %d, whatever became of it", i+2, got, i)
		}
	}
	if len(bounds.waits) != 8 || slices.ContainsFunc(bounds.waits, func(wait time.Duration) bool { return wait != 2*time.Second }) {
		t.Fatalf("the pass bounded its wait %v; want two seconds, in each of its eight units of work", bounds.waits)
	}

	err := tallied("past their deadline", tally)
	wantErr := "3 requests past their deadline could not be closed (3 closed); the first: request " + requests[0].ID.String() + ": its ledger row no longer waits"
	if err == nil || err.Error() != wantErr || errors.Is(err, apierr.ErrInvalidArgument) || errors.Is(err, repocontracts.ErrDeviationRequestDecided) {
		t.Fatalf("the pass answers\n  %v\nwant\n  %s", err, wantErr)
	}
}

// A pass is bounded: it makes as many attempts as it may and stops, leaving
// the rest to the next pass, and says that the budget stopped it. That alone
// is not a failure. A read that fails, and a context that ends, stop it too,
// and those are what it answers.
func TestAPassStopsAtItsBudgetAndAtWhatItCannotRead(t *testing.T) {
	closes := func(context.Context, entities.DeviationRequest) error { return nil }
	service := func(budget int) *deviationRequestService {
		return &deviationRequestService{repo: sweptStore{bounds: &boundsCounted{}}, sweepLockWait: time.Second, sweepBudget: budget}
	}

	_, read, cursors := overdueInOrder(7)
	tally := service(4).sweep(context.Background(), read, closes)
	if tally.closed != 4 || tally.failed != 0 || !tally.spent || tally.stopped != nil || len(*cursors) != 4 {
		t.Fatalf("a pass allowed four attempts tallied %+v after %d reads; want four closed, and stopped by its budget", tally, len(*cursors))
	}
	if err := tallied("past their deadline", tally); err != nil {
		t.Fatalf("a pass that used its budget and failed on nothing answers %v; want no error — it is said in the log, and the next pass goes on", err)
	}
	// A budget that is exactly enough is not spent: the pass read everything.
	_, read, _ = overdueInOrder(3)
	if tally := service(4).sweep(context.Background(), read, closes); tally.closed != 3 || tally.spent {
		t.Fatalf("a pass over three with a budget of four tallied %+v; want three closed and the budget not spent", tally)
	}

	away := errors.New("the database is away")
	requests, read, _ := overdueInOrder(5)
	reads := 0
	failing := func(ctx context.Context, after repocontracts.SweepCursor, limit int) ([]entities.DeviationRequest, error) {
		if reads++; reads == 3 {
			return nil, away
		}
		return read(ctx, after, limit)
	}
	tally = service(100).sweep(context.Background(), failing, func(_ context.Context, request entities.DeviationRequest) error {
		if request.ID == requests[0].ID {
			return errors.New("its ledger row no longer waits")
		}
		return nil
	})
	if tally.closed != 1 || tally.failed != 1 || !errors.Is(tally.stopped, away) || tally.spent {
		t.Fatalf("a pass whose third read failed tallied %+v; want one closed, one not, and stopped by the read", tally)
	}
	if err := tallied("past their deadline", tally); err == nil || !strings.HasPrefix(err.Error(), "the database is away (and before that, 1 request past their deadline could not be closed") {
		t.Fatalf("it answers %v; want the failure to read, and what the pass had left by then", err)
	}

	ended, stop := context.WithCancel(context.Background())
	stop()
	_, read, cursors = overdueInOrder(3)
	tally = service(100).sweep(ended, read, closes)
	if tally.closed != 0 || !errors.Is(tally.stopped, context.Canceled) || len(*cursors) != 0 {
		t.Fatalf("a pass whose context had ended tallied %+v after %d reads; want nothing read", tally, len(*cursors))
	}
	if err := tallied("past their deadline", tally); !errors.Is(err, context.Canceled) {
		t.Fatalf("it answers %v, want the context's end", err)
	}
	if err := tallied("past their deadline", sweepTally{closed: 9}); err != nil {
		t.Fatalf("a pass that closed everything it read answers %v", err)
	}
}
