package impl

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/singleflight"
)

// defaultInstanceCountTTL is how long the instance list's status counts are
// reused. A second: long enough that a page of the list opened by many people
// at once is one count, short enough that nobody acting on the list sees a
// number from before their own last click.
const defaultInstanceCountTTL = time.Second

// maxInstanceCountTTL bounds the setting. The counts are a convenience on the
// list, but a total read off them decides where its last page ends, so a
// stale one should not outlive a page reload by much.
const maxInstanceCountTTL = time.Minute

// instanceCountCacheSize bounds the entries held. Each is a handful of states
// and their counts; the bound is for the number of distinct (organization,
// project, filter) combinations seen inside one TTL, which is the number of
// people paging different lists in the same second.
const instanceCountCacheSize = 4096

const envInstanceCountTTL = "METIS_INSTANCE_COUNT_CACHE_TTL"

// instanceCountCache reuses the instance list's status counts for a moment.
//
// Every page of the list asks how many of the project's instances are in each
// state, and that count reads every instance the project has: it is O(project)
// on every request, and an index does not change that under a write load,
// because the rows it would skip are the ones being written. So the count is
// taken at most once per TTL for each scope and filter, and requests that ask
// while it is being taken wait for that one rather than starting their own.
//
// Keyed by tenant, as the definition cache is, and by environment: the
// repository scopes the count to the caller's organization and reads the
// environment's database, so an entry is only ever served under the scope and
// the database it was read from. The scope is the organization's projects,
// not the caller's, so two people in one organization asking about one project
// are asking the same question.
type instanceCountCache struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	entries map[string]instanceCounts
	reading singleflight.Group
}

type instanceCounts struct {
	counts  map[models.ProcessStatus]int64
	expires time.Time
}

func newInstanceCountCache() *instanceCountCache {
	return &instanceCountCache{
		ttl:     instanceCountTTL(),
		now:     time.Now,
		entries: make(map[string]instanceCounts),
	}
}

// instanceCountTTL reads the setting. Zero turns the cache off; anything that
// is not a duration from zero to a minute keeps the default, and says so.
func instanceCountTTL() time.Duration {
	raw := envvar.Get(envInstanceCountTTL)
	if raw == "" {
		return defaultInstanceCountTTL
	}
	ttl, err := time.ParseDuration(raw)
	if err != nil || ttl < 0 || ttl > maxInstanceCountTTL {
		log.Warn().Str("value", raw).Dur("default", defaultInstanceCountTTL).
			Msg(envInstanceCountTTL + " is not a duration from 0s to 1m; using the default")
		return defaultInstanceCountTTL
	}
	return ttl
}

// countKey names what a count was read for: the scope, the database, the
// project and the filter, all of it.
func countKey(ctx context.Context, projectID uuid.UUID, f repocontracts.InstanceFilter) string {
	environment, _ := db.EnvironmentFrom(ctx)
	return fmt.Sprintf("%s|%s|%s|%+v", scopeKey(ctx), environment, projectID, f)
}

// get returns the counts for key, reading them only when none fresh are held.
//
// The map returned is the caller's own: callers hand it to the response, and a
// shared one would be one mutation away from every other caller's answer.
func (c *instanceCountCache) get(
	ctx context.Context,
	key string,
	read func(context.Context) (map[models.ProcessStatus]int64, error),
) (map[models.ProcessStatus]int64, error) {
	if c.ttl <= 0 {
		return read(ctx)
	}
	if counts, ok := c.fresh(key); ok {
		return counts, nil
	}
	shared, err, _ := c.reading.Do(key, func() (any, error) {
		if counts, ok := c.fresh(key); ok {
			return counts, nil
		}
		// Expiry runs from when the read started, so an entry is never older
		// than the TTL however long the count took.
		started := c.now()
		// Not cancelled with the caller who happened to start it: the others
		// waiting on it did not hang up. The scope and the environment are
		// values on the context, and survive this.
		counts, err := read(context.WithoutCancel(ctx))
		if err != nil {
			return nil, err
		}
		c.put(key, counts, started.Add(c.ttl))
		return counts, nil
	})
	if err != nil {
		return nil, err
	}
	counts, ok := shared.(map[models.ProcessStatus]int64)
	if !ok {
		return nil, fmt.Errorf("instance counts: unexpected %T from the shared read", shared)
	}
	return maps.Clone(counts), nil
}

func (c *instanceCountCache) fresh(key string) (map[models.ProcessStatus]int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || !c.now().Before(entry.expires) {
		return nil, false
	}
	return maps.Clone(entry.counts), true
}

func (c *instanceCountCache) put(key string, counts map[models.ProcessStatus]int64, expires time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= instanceCountCacheSize {
		now := c.now()
		for k, entry := range c.entries {
			if !now.Before(entry.expires) {
				delete(c.entries, k)
			}
		}
		// Every entry still fresh: this many lists in one TTL is not a cache
		// working set. Starting over costs a count each, which is what they
		// would cost uncached.
		if len(c.entries) >= instanceCountCacheSize {
			clear(c.entries)
		}
	}
	c.entries[key] = instanceCounts{counts: maps.Clone(counts), expires: expires}
}
