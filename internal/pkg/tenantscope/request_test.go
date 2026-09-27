package tenantscope

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

// counter is a project-list read that counts how often it is made.
type counter struct {
	reads    atomic.Int64
	projects []uuid.UUID
	err      error
}

func (c *counter) read() ([]uuid.UUID, error) {
	c.reads.Add(1)
	return c.projects, c.err
}

func newCounter() *counter {
	return &counter{projects: []uuid.UUID{uuid.New(), uuid.New()}}
}

const (
	own   = "0199a000-0000-7000-8000-000000000001"
	other = "0199a000-0000-7000-8000-000000000002"
)

func TestARequestReadsItsOrganizationsProjectsOnce(t *testing.T) {
	ctx, request := WithRequest(t.Context(), own)
	defer request.End()
	c := newCounter()

	for range 5 {
		got, err := RequestFrom(ctx).Projects(own, c.read)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(got, c.projects) {
			t.Fatalf("got %v, want %v", got, c.projects)
		}
	}
	if n := c.reads.Load(); n != 1 {
		t.Fatalf("five scoped calls in one request read the project list %d times, want 1", n)
	}
}

// Everything that is not the request's own organization, or not in a request at
// all, is read every time — the behaviour every call had before, and the only
// one that cannot answer one tenant from another's list.
func TestWhatIsNotTheRequestsOwnIsReadEveryTime(t *testing.T) {
	for _, tc := range []struct {
		name string
		ctx  func(t *testing.T) context.Context
		org  string
	}{
		{"no request", func(t *testing.T) context.Context { return t.Context() }, own},
		{"another organization", func(t *testing.T) context.Context {
			ctx, request := WithRequest(t.Context(), own)
			t.Cleanup(request.End)
			return ctx
		}, other},
		{"a request that has ended", func(t *testing.T) context.Context {
			ctx, request := WithRequest(t.Context(), own)
			request.End()
			return ctx
		}, own},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, c := tc.ctx(t), newCounter()
			for range 3 {
				if _, err := RequestFrom(ctx).Projects(tc.org, c.read); err != nil {
					t.Fatal(err)
				}
			}
			if n := c.reads.Load(); n != 3 {
				t.Fatalf("three calls read the project list %d times, want 3", n)
			}
		})
	}
}

func TestAnotherOrganizationDoesNotDisplaceTheRequestsOwn(t *testing.T) {
	ctx, request := WithRequest(t.Context(), own)
	defer request.End()
	mine, theirs := newCounter(), newCounter()

	if _, err := request.Projects(own, mine.read); err != nil {
		t.Fatal(err)
	}
	got, err := request.Projects(other, theirs.read)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, theirs.projects) {
		t.Fatalf("the other organization was answered with %v, want its own %v", got, theirs.projects)
	}
	got, err = RequestFrom(ctx).Projects(own, mine.read)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, mine.projects) || mine.reads.Load() != 1 {
		t.Fatalf("the request's own organization got %v after %d reads, want %v after 1",
			got, mine.reads.Load(), mine.projects)
	}
}

func TestAFailedReadIsNotKept(t *testing.T) {
	_, request := WithRequest(t.Context(), own)
	defer request.End()
	c := newCounter()
	c.err = errors.New("the database went away")

	if _, err := request.Projects(own, c.read); !errors.Is(err, c.err) {
		t.Fatalf("got %v, want the read's error", err)
	}
	c.err = nil
	got, err := request.Projects(own, c.read)
	if err != nil || !slices.Equal(got, c.projects) {
		t.Fatalf("after a failed read got %v, %v; want the projects", got, err)
	}
	if n := c.reads.Load(); n != 2 {
		t.Fatalf("read %d times, want 2: the failure must not stand in for the list", n)
	}
}

func TestForgetMakesTheNextCallReadAgain(t *testing.T) {
	_, request := WithRequest(t.Context(), own)
	defer request.End()
	c := newCounter()

	if _, err := request.Projects(own, c.read); err != nil {
		t.Fatal(err)
	}
	c.projects = append(slices.Clone(c.projects), uuid.New())
	request.Forget()
	got, err := request.Projects(own, c.read)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, c.projects) {
		t.Fatalf("after Forget got %v, want the new list %v", got, c.projects)
	}
}

// A read that was under way when the projects changed read the old list, so it
// is returned to its caller but not kept for the next one.
func TestAReadThatRacedAChangeIsNotKept(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*Request)
	}{
		{"forgotten", (*Request).Forget},
		{"ended", (*Request).End},
	} {
		t.Run(change.name, func(t *testing.T) {
			_, request := WithRequest(t.Context(), own)
			defer request.End()
			c := newCounter()
			racing := func() ([]uuid.UUID, error) {
				change.apply(request)
				return c.read()
			}
			if _, err := request.Projects(own, racing); err != nil {
				t.Fatal(err)
			}
			if _, err := request.Projects(own, c.read); err != nil {
				t.Fatal(err)
			}
			if n := c.reads.Load(); n != 2 {
				t.Fatalf("read %d times, want 2: the read that raced the change was kept", n)
			}
		})
	}
}

func TestACallerAppendingToTheListCannotChangeWhatTheNextIsHanded(t *testing.T) {
	_, request := WithRequest(t.Context(), own)
	defer request.End()
	c := newCounter()
	c.projects = make([]uuid.UUID, 2, 10)
	c.projects[0], c.projects[1] = uuid.New(), uuid.New()

	if _, err := request.Projects(own, c.read); err != nil {
		t.Fatal(err)
	}
	kept, err := request.Projects(own, c.read)
	if err != nil {
		t.Fatal(err)
	}
	// No room past the end, so an append copies instead of writing into the
	// array every other caller in the request is reading.
	if len(kept) != 2 || cap(kept) != len(kept) {
		t.Fatalf("the kept list has length %d and capacity %d, want both 2", len(kept), cap(kept))
	}
}

func TestANilRequestKeepsNothing(t *testing.T) {
	var request *Request
	c := newCounter()
	for range 2 {
		if _, err := request.Projects(own, c.read); err != nil {
			t.Fatal(err)
		}
	}
	request.Forget()
	request.End()
	if n := c.reads.Load(); n != 2 {
		t.Fatalf("read %d times, want 2", n)
	}
}

// Scoped calls from several goroutines of one request share the one list. Run
// under -race, this is also the proof that they may.
func TestConcurrentCallsInOneRequestShareTheList(t *testing.T) {
	_, request := WithRequest(t.Context(), own)
	defer request.End()
	c := newCounter()
	if _, err := request.Projects(own, c.read); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 100 {
				got, err := request.Projects(own, c.read)
				if err != nil || !slices.Equal(got, c.projects) {
					t.Errorf("got %v, %v", got, err)
					return
				}
			}
		})
	}
	wg.Go(request.Forget)
	wg.Wait()
	if n := c.reads.Load(); n > 17 {
		t.Fatalf("read %d times for 1,600 calls and one change", n)
	}
}
