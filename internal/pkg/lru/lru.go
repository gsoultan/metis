// Package lru is a bounded, least-recently-used cache.
//
// It exists because the caches in this codebase were bounded by refusing to
// grow rather than by evicting: past the limit, new entries were simply never
// remembered. That is safe for memory and quietly terrible in practice — one
// tenant deploying four thousand distinct expressions turns caching off for
// everybody else, permanently, with nothing in the logs to say so.
//
// Every cache here is keyed by something that ultimately comes from a process
// definition, which is user-authored. A map keyed by attacker-supplied input
// needs both a bound and an eviction; this provides both.
package lru

import (
	"container/list"
	"sync"
)

// Cache is a fixed-capacity map that evicts the least recently used entry.
// It is safe for concurrent use.
type Cache[K comparable, V any] struct {
	mu       sync.Mutex
	capacity int
	entries  map[K]*list.Element
	order    *list.List // front is most recently used
	// evicted is told about every entry the cache lets go of, or is nil.
	evicted func(K, V)
}

type pair[K comparable, V any] struct {
	key   K
	value V
}

// entryOf reads the pair a list element carries.
//
// container/list stores `any`, so the assertion has to happen somewhere; this is
// the one place it does. It cannot fail — nothing but Put ever pushes onto the
// list, and it only ever pushes *pair[K, V] — but it is written comma-ok anyway
// rather than silenced, because the rule that flags it exists for a real reason:
// an unchecked assertion on a value that *could* be another shape is how a
// worker panics on a process variable. A cache degrading to a miss is the right
// behaviour for an invariant that has somehow been broken; a panic is not.
func entryOf[K comparable, V any](element *list.Element) (*pair[K, V], bool) {
	if element == nil {
		return nil, false
	}
	entry, ok := element.Value.(*pair[K, V])
	return entry, ok
}

// New returns a cache holding at most capacity entries. A capacity below one is
// raised to one: a cache that can hold nothing is a bug at the call site, and
// silently disabling it there would be the same fail-open this package exists
// to remove.
// NewWithEviction is New with a function told about every entry the cache
// lets go of — pushed out to stay within capacity, removed, or cleared — for a
// value that holds something to release. A connection pool dropped from a cache
// without being closed keeps its connections open for good.
//
// It is called after the cache's lock is released, so it may take its time
// without holding up every other caller.
func NewWithEviction[K comparable, V any](capacity int, evicted func(K, V)) *Cache[K, V] {
	c := New[K, V](capacity)
	c.evicted = evicted
	return c
}

func New[K comparable, V any](capacity int) *Cache[K, V] {
	if capacity < 1 {
		capacity = 1
	}
	return &Cache[K, V]{
		capacity: capacity,
		entries:  make(map[K]*list.Element, capacity),
		order:    list.New(),
	}
}

// Get returns the value for key and marks it most recently used.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		var zero V
		return zero, false
	}
	entry, ok := entryOf[K, V](element)
	if !ok {
		var zero V
		return zero, false
	}
	c.order.MoveToFront(element)
	return entry.value, true
}

// Put stores a value, evicting the least recently used entry when full.
func (c *Cache[K, V]) Put(key K, value V) {
	c.release(c.put(key, value))
}

// put stores the value and returns whatever it pushed out, so the eviction
// function runs after the lock is released rather than while every other caller
// waits on it.
func (c *Cache[K, V]) put(key K, value V) []*pair[K, V] {
	c.mu.Lock()
	defer c.mu.Unlock()

	if element, held := c.entries[key]; held {
		if entry, ok := entryOf[K, V](element); ok {
			entry.value = value
			c.order.MoveToFront(element)
			return nil
		}
		// Unreachable: drop the corrupt element and fall through to store the
		// value afresh, so a broken invariant costs a re-computation rather than
		// a wrong answer that never expires.
		c.order.Remove(element)
		delete(c.entries, key)
	}

	c.entries[key] = c.order.PushFront(&pair[K, V]{key: key, value: value})

	var dropped []*pair[K, V]
	for c.order.Len() > c.capacity {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		if entry, ok := entryOf[K, V](oldest); ok {
			delete(c.entries, entry.key)
			dropped = append(dropped, entry)
		}
	}
	return dropped
}

// release tells the eviction function about entries already out of the cache.
func (c *Cache[K, V]) release(dropped []*pair[K, V]) {
	if c.evicted == nil {
		return
	}
	for _, entry := range dropped {
		c.evicted(entry.key, entry.value)
	}
}

// Remove drops an entry. Used when the thing behind it has changed or gone —
// a cache that outlives its source is how a deleted definition keeps running.
func (c *Cache[K, V]) Remove(key K) {
	c.release(c.remove(key))
}

func (c *Cache[K, V]) remove(key K) []*pair[K, V] {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return nil
	}
	c.order.Remove(element)
	delete(c.entries, key)
	if entry, ok := entryOf[K, V](element); ok {
		return []*pair[K, V]{entry}
	}
	return nil
}

// Clear empties the cache.
func (c *Cache[K, V]) Clear() {
	c.release(c.clear())
}

func (c *Cache[K, V]) clear() []*pair[K, V] {
	c.mu.Lock()
	defer c.mu.Unlock()
	var dropped []*pair[K, V]
	for element := c.order.Front(); element != nil; element = element.Next() {
		if entry, ok := entryOf[K, V](element); ok {
			dropped = append(dropped, entry)
		}
	}
	c.entries = make(map[K]*list.Element, c.capacity)
	c.order.Init()
	return dropped
}

// Len reports how many entries are held.
func (c *Cache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
