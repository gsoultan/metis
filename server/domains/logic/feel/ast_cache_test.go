package feel

import (
	"fmt"
	"sync"
	"testing"
)

func hotLen(c *astCache) int {
	n := 0
	c.hot.Range(func(any, any) bool { n++; return true })
	return n
}

// The lock-free read path holds no more than the LRU does: an entry the LRU
// evicts leaves hot too, so untrusted expressions cannot grow it without bound.
func TestTheCachesLockFreeReadsStayWithinItsBound(t *testing.T) {
	c := &astCache{}
	var wg sync.WaitGroup
	for w := range 8 {
		wg.Go(func() {
			for i := range 3 * maxCachedExpressions {
				text := fmt.Sprintf("x > %d", (i*7+w)%(4*maxCachedExpressions))
				if _, err := c.parse(text, Parse); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	if got := hotLen(c); got > maxCachedExpressions {
		t.Fatalf("hot holds %d entries, more than the LRU's bound of %d", got, maxCachedExpressions)
	}
	if got := c.cache().Len(); got > maxCachedExpressions {
		t.Fatalf("the LRU holds %d entries", got)
	}
}

func TestACachedParseIsTheSameTree(t *testing.T) {
	c := &astCache{}
	first, err := c.parse("a + b > 3", Parse)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := c.parse("a + b > 3", Parse)
	if fmt.Sprintf("%p", first) != fmt.Sprintf("%p", second) {
		t.Fatal("a second parse of the same text did not come from the cache")
	}
	if _, err := c.parse("a +", Parse); err == nil {
		t.Fatal("a broken expression parsed")
	}
	if _, err := c.parse("a +", Parse); err == nil {
		t.Fatal("a cached failure came back as a success")
	}
}

// BenchmarkCachedCellsInParallel is a decision table's load: the same cells,
// on every core.
func BenchmarkCachedCellsInParallel(b *testing.B) {
	cells := make([]string, 300)
	for i := range cells {
		cells[i] = fmt.Sprintf("[%d..%d]", i, i+10)
	}
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if _, err := EvaluateUnaryTests(cells[i%len(cells)], 5.0, nil, nil); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}
