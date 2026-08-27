package security

import (
	"context"
	"sync"
	"time"
)

// memoryIdempotencyStore keeps records in the serving process.
//
// This is the right store for a single replica — the supported topology — and
// the wrong one for more than that, because a retry reaching a different
// replica finds an empty map and executes the write again. Deployments running
// more than one process want the database-backed store.
type memoryIdempotencyStore struct {
	ttl time.Duration
	now func() time.Time

	mu           sync.Mutex
	entries      map[string]*memoryEntry
	requestCount int
}

type memoryEntry struct {
	requestHash string
	createdAt   time.Time
	done        chan struct{}
	response    *StoredResponse
}

// NewMemoryIdempotencyStore returns a store held in this process.
func NewMemoryIdempotencyStore(ttl time.Duration) IdempotencyStore {
	if ttl <= 0 {
		ttl = defaultIdempotencyTTL
	}
	return &memoryIdempotencyStore{
		ttl:     ttl,
		now:     time.Now,
		entries: make(map[string]*memoryEntry, idempotencyCleanupEvery),
	}
}

func (s *memoryIdempotencyStore) Claim(_ context.Context, key, requestHash string) (ClaimOutcome, error) {
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Sweeping on a request count rather than a timer keeps this free of a
	// background goroutine whose lifetime nothing owns.
	s.requestCount++
	if s.requestCount%idempotencyCleanupEvery == 0 {
		s.sweep(now.Add(-s.ttl))
	}

	if existing, ok := s.entries[key]; ok {
		if !s.expired(existing, now) {
			if existing.requestHash != requestHash {
				return ClaimOutcome{Conflict: true}, nil
			}
			if existing.response != nil {
				return ClaimOutcome{Response: existing.response}, nil
			}
			return ClaimOutcome{}, nil // in flight here
		}
		delete(s.entries, key)
	}

	s.entries[key] = &memoryEntry{requestHash: requestHash, createdAt: now, done: make(chan struct{})}
	return ClaimOutcome{Owned: true}, nil
}

func (s *memoryIdempotencyStore) Complete(_ context.Context, key string, response StoredResponse) error {
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[key]
	if !ok || entry.response != nil {
		return nil
	}
	stored := response
	entry.response = &stored
	entry.createdAt = now
	close(entry.done)
	return nil
}

// Abandon drops a claim nobody will complete, releasing anyone waiting on it.
func (s *memoryIdempotencyStore) Abandon(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[key]
	if !ok || entry.response != nil {
		return nil
	}
	delete(s.entries, key)
	// Waiters see a closed channel and no response, which they report as an
	// unavailable answer rather than hanging until their own deadline.
	close(entry.done)
	return nil
}

func (s *memoryIdempotencyStore) Await(ctx context.Context, key string) (*StoredResponse, error) {
	s.mu.Lock()
	entry, ok := s.entries[key]
	s.mu.Unlock()
	if !ok {
		return nil, nil
	}

	budget, cancel := context.WithTimeout(ctx, idempotencyWaitBudget)
	defer cancel()

	select {
	case <-entry.done:
		s.mu.Lock()
		response := entry.response
		s.mu.Unlock()
		return response, nil
	case <-budget.Done():
		return nil, budget.Err()
	}
}

func (s *memoryIdempotencyStore) sweep(before time.Time) {
	for key, entry := range s.entries {
		if entry.response != nil && entry.createdAt.Before(before) {
			delete(s.entries, key)
		}
	}
}

// expired reports whether a *completed* record has aged out. An incomplete one
// never expires by age: it means work is in flight, and forgetting it would let
// a second caller start the same work.
func (s *memoryIdempotencyStore) expired(entry *memoryEntry, now time.Time) bool {
	return entry.response != nil && now.Sub(entry.createdAt) > s.ttl
}
