package lru

import (
	"fmt"
	"sync"
	"testing"
)

func TestHoldsAndReturns(t *testing.T) {
	c := New[string, int](2)
	c.Put("a", 1)
	if got, ok := c.Get("a"); !ok || got != 1 {
		t.Fatalf("got %v %v, want 1 true", got, ok)
	}
	if _, ok := c.Get("missing"); ok {
		t.Fatal("reported a value it was never given")
	}
}

/*
 * The behaviour this package exists for.
 *
 * The caches it replaces bounded themselves by refusing to grow, so past the
 * limit nothing new was ever cached again — one busy tenant turned caching off
 * for the whole installation. Evicting the least recently used entry keeps the
 * cache useful at the bound.
 */
func TestEvictsTheLeastRecentlyUsed(t *testing.T) {
	c := New[string, int](2)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Get("a") // "a" is now the most recently used, so "b" is next out
	c.Put("c", 3)

	if _, ok := c.Get("b"); ok {
		t.Fatal("kept the least recently used entry")
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatal("evicted a recently used entry")
	}
	if _, ok := c.Get("c"); !ok {
		t.Fatal("did not keep the newest entry")
	}
	if c.Len() != 2 {
		t.Fatalf("holding %d entries, want 2", c.Len())
	}
}

func TestNeverGrowsPastCapacity(t *testing.T) {
	c := New[int, int](8)
	for i := range 1000 {
		c.Put(i, i)
	}
	if c.Len() != 8 {
		t.Fatalf("holding %d entries, want 8", c.Len())
	}
}

func TestPutReplacesWithoutGrowing(t *testing.T) {
	c := New[string, int](2)
	c.Put("a", 1)
	c.Put("a", 2)
	if got, _ := c.Get("a"); got != 2 {
		t.Fatalf("got %d, want the replacement", got)
	}
	if c.Len() != 1 {
		t.Fatalf("holding %d entries, want 1", c.Len())
	}
}

// A cache that outlives its source is how a deleted definition keeps running.
func TestRemoveAndClear(t *testing.T) {
	c := New[string, int](4)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Remove("a")
	if _, ok := c.Get("a"); ok {
		t.Fatal("removed entry is still readable")
	}
	c.Clear()
	if c.Len() != 0 {
		t.Fatalf("cleared cache holds %d entries", c.Len())
	}
}

func TestZeroCapacityStillCaches(t *testing.T) {
	// A capacity of zero would be a fail-open cache that silently never holds
	// anything, which is the failure mode this package removes.
	c := New[string, int](0)
	c.Put("a", 1)
	if _, ok := c.Get("a"); !ok {
		t.Fatal("a zero-capacity cache held nothing at all")
	}
}

func TestConcurrentUseIsSafe(t *testing.T) {
	c := New[string, int](64)
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := range 200 {
				key := fmt.Sprintf("k%d", (worker*200+i)%100)
				c.Put(key, i)
				c.Get(key)
			}
		}(worker)
	}
	wg.Wait()
	if c.Len() > 64 {
		t.Fatalf("holding %d entries, want at most 64", c.Len())
	}
}

// A value that holds something — a connection pool — has to be told when the
// cache lets go of it, by any path, or what it holds is never released.
func TestEvictedEntriesAreHandedBack(t *testing.T) {
	var released []int
	c := NewWithEviction[string, int](2, func(_ string, v int) { released = append(released, v) })

	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3) // pushes out a
	if len(released) != 1 || released[0] != 1 {
		t.Fatalf("pushed out %v, want [1]", released)
	}
	c.Put("c", 30) // a replacement is not an eviction
	if len(released) != 1 {
		t.Fatalf("replacing a value released %v", released)
	}
	c.Remove("b")
	if len(released) != 2 || released[1] != 2 {
		t.Fatalf("Remove released %v, want [1 2]", released)
	}
	c.Clear()
	if len(released) != 3 || released[2] != 30 {
		t.Fatalf("Clear released %v, want [1 2 30]", released)
	}
}

// The eviction function runs outside the lock, so one that uses the cache —
// or simply takes a while — does not deadlock or stall other callers.
func TestTheEvictionFunctionMayUseTheCache(t *testing.T) {
	var c *Cache[string, int]
	c = NewWithEviction[string, int](1, func(string, int) { _ = c.Len() })
	c.Put("a", 1)
	c.Put("b", 2) // would deadlock if the callback ran under the lock
	c.Remove("b")
	c.Clear()
}
