package impl

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
)

// countingRead is a count that says how often it was taken.
type countingRead struct {
	reads atomic.Int64
	value int64
}

func (r *countingRead) read(context.Context) (map[models.ProcessStatus]int64, error) {
	r.reads.Add(1)
	return map[models.ProcessStatus]int64{models.ProcessActive: r.value}, nil
}

func cacheAt(clock *time.Time, ttl time.Duration) *instanceCountCache {
	c := newInstanceCountCache()
	c.ttl = ttl
	c.now = func() time.Time { return *clock }
	return c
}

func tenant(organization string) context.Context {
	return entities.WithTenantContext(context.Background(), entities.TenantContext{TenantID: organization})
}

// Inside the TTL the count is taken once; after it, again.
func TestTheCountsAreReusedForTheirTTLAndThenReadAgain(t *testing.T) {
	clock := time.Unix(1_000, 0)
	c := cacheAt(&clock, time.Second)
	read := &countingRead{value: 3}
	ctx := tenant(uuid.NewString())
	key := countKey(ctx, uuid.New(), repocontracts.InstanceFilter{})

	for range 5 {
		if _, err := c.get(ctx, key, read.read); err != nil {
			t.Fatal(err)
		}
	}
	if n := read.reads.Load(); n != 1 {
		t.Fatalf("five reads inside the TTL took the count %d times, want once", n)
	}

	clock = clock.Add(time.Second)
	read.value = 4
	counts, err := c.get(ctx, key, read.read)
	if err != nil {
		t.Fatal(err)
	}
	if n := read.reads.Load(); n != 2 || counts[models.ProcessActive] != 4 {
		t.Fatalf("after the TTL: %d reads and %d active, want 2 and 4", n, counts[models.ProcessActive])
	}
}

// One organization's count is never another's, nor one environment's another's,
// nor one project's or filter's another's.
func TestTheCountsAreHeldPerScopeEnvironmentProjectAndFilter(t *testing.T) {
	project, definition := uuid.New(), uuid.New()
	ours := tenant(uuid.NewString())
	keys := map[string]string{
		"ours":                countKey(ours, project, repocontracts.InstanceFilter{}),
		"another tenant":      countKey(tenant(uuid.NewString()), project, repocontracts.InstanceFilter{}),
		"system work":         countKey(entities.WithSystemContext(context.Background()), project, repocontracts.InstanceFilter{}),
		"unscoped":            countKey(context.Background(), project, repocontracts.InstanceFilter{}),
		"another environment": countKey(db.Bind(ours, uuid.New()), project, repocontracts.InstanceFilter{}),
		"another project":     countKey(ours, uuid.New(), repocontracts.InstanceFilter{}),
		"every project":       countKey(ours, uuid.Nil, repocontracts.InstanceFilter{}),
		"one definition":      countKey(ours, project, repocontracts.InstanceFilter{DefinitionID: definition}),
		"one state":           countKey(ours, project, repocontracts.InstanceFilter{Status: models.ProcessActive}),
		"a business key":      countKey(ours, project, repocontracts.InstanceFilter{BusinessKey: "order-7"}),
		"needs attention":     countKey(ours, project, repocontracts.InstanceFilter{NeedsAttention: true}),
	}
	seen := map[string]string{}
	for name, key := range keys {
		if other, taken := seen[key]; taken {
			t.Errorf("%q and %q share the key %q", name, other, key)
		}
		seen[key] = name
	}
	if again := countKey(ours, project, repocontracts.InstanceFilter{}); again != keys["ours"] {
		t.Errorf("the same question keyed %q, then %q", keys["ours"], again)
	}
}

// Callers asking while the count is being taken wait for that one.
func TestCallersAskingAtOnceShareOneCount(t *testing.T) {
	c := newInstanceCountCache()
	c.ttl = time.Minute
	release := make(chan struct{})
	var reads atomic.Int64
	read := func(context.Context) (map[models.ProcessStatus]int64, error) {
		reads.Add(1)
		<-release
		return map[models.ProcessStatus]int64{models.ProcessActive: 1}, nil
	}
	ctx := tenant(uuid.NewString())
	key := countKey(ctx, uuid.New(), repocontracts.InstanceFilter{})

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			if counts, err := c.get(ctx, key, read); err != nil || counts[models.ProcessActive] != 1 {
				t.Errorf("got %v, %v", counts, err)
			}
		})
	}
	// Let the first caller in and the rest pile up behind it.
	for reads.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := reads.Load(); n != 1 {
		t.Fatalf("fifty callers at once took the count %d times, want once", n)
	}
}

// The caller who started the shared count hanging up does not fail the others.
func TestTheSharedCountOutlivesTheCallerWhoStartedIt(t *testing.T) {
	c := newInstanceCountCache()
	c.ttl = time.Minute
	ctx, cancel := context.WithCancel(tenant(uuid.NewString()))
	cancel()
	read := func(ctx context.Context) (map[models.ProcessStatus]int64, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return map[models.ProcessStatus]int64{models.ProcessActive: 1}, nil
	}
	if _, err := c.get(ctx, countKey(ctx, uuid.New(), repocontracts.InstanceFilter{}), read); err != nil {
		t.Fatalf("the count was cancelled with its caller: %v", err)
	}
}

// A failed count is not remembered, and what a caller is handed is its own.
func TestAFailureIsNotHeldAndTheCountsAreTheCallersOwn(t *testing.T) {
	clock := time.Unix(1_000, 0)
	c := cacheAt(&clock, time.Minute)
	ctx := tenant(uuid.NewString())
	key := countKey(ctx, uuid.New(), repocontracts.InstanceFilter{})

	failing := func(context.Context) (map[models.ProcessStatus]int64, error) {
		return nil, errors.New("the database went away")
	}
	if _, err := c.get(ctx, key, failing); err == nil {
		t.Fatal("a failed count reported no error")
	}
	read := &countingRead{value: 2}
	counts, err := c.get(ctx, key, read.read)
	if err != nil || read.reads.Load() != 1 {
		t.Fatalf("after a failure the next caller did not count: %v, %d reads", err, read.reads.Load())
	}

	counts[models.ProcessActive] = 99
	again, err := c.get(ctx, key, read.read)
	if err != nil {
		t.Fatal(err)
	}
	if again[models.ProcessActive] != 2 {
		t.Fatalf("one caller's change reached the next: %d active, want 2", again[models.ProcessActive])
	}
}

// Zero turns the reuse off; a value out of range keeps the default.
func TestTheTTLSetting(t *testing.T) {
	for raw, want := range map[string]time.Duration{
		"":      defaultInstanceCountTTL,
		"0":     0,
		"0s":    0,
		"250ms": 250 * time.Millisecond,
		"1m":    time.Minute,
		"2m":    defaultInstanceCountTTL,
		"-1s":   defaultInstanceCountTTL,
		"soon":  defaultInstanceCountTTL,
	} {
		t.Setenv(envInstanceCountTTL, raw)
		if got := instanceCountTTL(); got != want {
			t.Errorf("%q: %v, want %v", raw, got, want)
		}
	}

	clock := time.Unix(1_000, 0)
	c := cacheAt(&clock, 0)
	read := &countingRead{}
	ctx := tenant(uuid.NewString())
	key := countKey(ctx, uuid.New(), repocontracts.InstanceFilter{})
	for range 3 {
		if _, err := c.get(ctx, key, read.read); err != nil {
			t.Fatal(err)
		}
	}
	if n := read.reads.Load(); n != 3 {
		t.Fatalf("with the TTL at zero three reads took the count %d times, want 3", n)
	}
}
