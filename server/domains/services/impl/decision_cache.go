package impl

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/internal/pkg/lru"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/models"
	"github.com/rs/zerolog/log"
)

// Decision tables held for a moment.
//
// Every evaluation read its table from the database: the live pointer, then
// the version it named, and the same again for each decision it requires. On
// a 100-rule table the reads were about 95% of an evaluation — 999 a second
// with one worker, 1,402 with eight, against 52 µs for the table run itself.
// A version is never changed once saved, so the cost bought nothing but
// noticing a new live version at once.
//
// So a table read is reused for METIS_DECISION_CACHE_TTL. On this replica a
// save, a release or a delete clears the cache, so its own changes apply at
// once; another replica sees them within the TTL, which is the price of not
// reading. Zero turns the cache off.
//
// Keyed by tenant and environment as the definition and instance count caches
// are: the repository scopes the read to the caller's organization and reads
// the environment's database, so an entry is served only under the scope and
// from the database it was read under.

const (
	envDecisionCacheTTL     = "METIS_DECISION_CACHE_TTL"
	defaultDecisionCacheTTL = time.Second
	maxDecisionCacheTTL     = time.Minute
	// decisionCacheSize bounds the versions held: tables, each a few rules to
	// a few hundred, across every project a replica evaluates for.
	decisionCacheSize = 1024
)

type decisionCache struct {
	ttl     time.Duration
	now     func() time.Time
	entries *lru.Cache[string, cachedDecision]
}

type cachedDecision struct {
	model   models.DecisionDefinitionModel
	expires time.Time
}

func newDecisionCache() *decisionCache {
	return &decisionCache{
		ttl:     decisionCacheTTL(),
		now:     time.Now,
		entries: lru.New[string, cachedDecision](decisionCacheSize),
	}
}

// decisionCacheTTL reads the setting. Zero turns the cache off; anything that
// is not a duration from zero to a minute keeps the default, and says so.
func decisionCacheTTL() time.Duration {
	raw := envvar.Get(envDecisionCacheTTL)
	if raw == "" {
		return defaultDecisionCacheTTL
	}
	ttl, err := time.ParseDuration(raw)
	if err != nil || ttl < 0 || ttl > maxDecisionCacheTTL {
		log.Warn().Str("value", raw).Dur("default", defaultDecisionCacheTTL).
			Msg(envDecisionCacheTTL + " is not a duration from 0s to 1m; using the default")
		return defaultDecisionCacheTTL
	}
	return ttl
}

// get returns the table key names at version (zero: the live one), reading it
// only when none fresh is held. A failed read is not kept: a table that is
// not there yet may be saved in the next second.
//
// The model returned is shared with other callers. Never mutate it.
func (c *decisionCache) get(
	ctx context.Context,
	projectID uuid.UUID,
	key string,
	version int,
	read func() (models.DecisionDefinitionModel, error),
) (models.DecisionDefinitionModel, error) {
	if c.ttl <= 0 {
		return read()
	}
	environment, _ := db.EnvironmentFrom(ctx)
	cacheKey := fmt.Sprintf("%s|%s|%s|%d|%s", scopeKey(ctx), environment, projectID, version, key)
	if held, ok := c.entries.Get(cacheKey); ok && c.now().Before(held.expires) {
		return held.model, nil
	}
	started := c.now()
	m, err := read()
	if err != nil {
		return models.DecisionDefinitionModel{}, err
	}
	// Expiry runs from when the read started, so an entry is never older
	// than the TTL however long the read took.
	c.entries.Put(cacheKey, cachedDecision{model: m, expires: started.Add(c.ttl)})
	return m, nil
}

// forget drops every table held. Called after this replica saves, releases or
// deletes one.
func (c *decisionCache) forget() {
	c.entries.Clear()
}
