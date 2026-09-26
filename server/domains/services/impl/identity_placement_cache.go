package impl

import (
	"crypto/sha256"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/lru"
	"github.com/gsoultan/metis/server/domains/entities"
)

// identityPlacement is where a sign-in through an identity provider was placed:
// the account linked to the identity, and the organizations the token's claim
// admits it to, first the one its requests land in by default.
type identityPlacement struct {
	account       uuid.UUID
	organizations []uuid.UUID
}

// placementKey is a digest of everything a placement is decided from.
type placementKey [sha256.Size]byte

// cachedPlacement is a placement, or the refusal of one, and when it lapses.
type cachedPlacement struct {
	placement identityPlacement
	refusal   error
	expires   time.Time
}

// placementCache remembers, briefly, how a sign-in was placed.
//
// An ID token is presented on every request, and placing one reads the
// organizations its claim names, the account linked to it and that account's
// memberships. For the same identity and the same claim the answer does not
// change unless something else does, and the lifetime bounds how long that goes
// unseen: METIS_AUTH_CACHE_TTL, the one the account cache already uses, for the
// same reasons. A role change is not held up by it — the account itself is read
// through that cache, which forgets an account the moment it is edited.
//
// Keyed by a digest of the identity and the claim, so a token whose claim has
// changed is placed afresh at once. Refusals are kept too: somebody the provider
// places nowhere, retrying, would otherwise cost a lookup and a line in the log
// on every request.
type placementCache struct {
	entries *lru.Cache[placementKey, cachedPlacement]
	ttl     time.Duration
	now     func() time.Time
}

func newPlacementCache() *placementCache {
	return &placementCache{
		entries: lru.New[placementKey, cachedPlacement](principalCacheSize()),
		ttl:     principalCacheTTL(),
		now:     time.Now,
	}
}

// placementKeyOf digests the identity and the claim. Each part is written with
// its length, so no two different claims can run together into one digest.
func placementKeyOf(claims entities.IdentityClaims) placementKey {
	hasher := sha256.New()
	write := func(part string) {
		hasher.Write([]byte(strconv.Itoa(len(part)) + ":"))
		hasher.Write([]byte(part))
	}
	write(claims.Issuer)
	write(claims.Subject)
	write(claims.OrganizationClaim)
	if claims.HasOrganizationClaim {
		write("present")
	}
	for _, value := range claims.Organizations {
		write(value)
	}
	var key placementKey
	copy(key[:], hasher.Sum(nil))
	return key
}

// get returns a placement held and still fresh. A zero lifetime disables the
// cache, as it does the account cache.
func (c *placementCache) get(key placementKey) (cachedPlacement, bool) {
	if c.ttl <= 0 {
		return cachedPlacement{}, false
	}
	entry, ok := c.entries.Get(key)
	if !ok {
		return cachedPlacement{}, false
	}
	if !c.now().Before(entry.expires) {
		c.entries.Remove(key)
		return cachedPlacement{}, false
	}
	return entry, true
}

func (c *placementCache) put(key placementKey, placement identityPlacement, refusal error) {
	if c.ttl <= 0 {
		return
	}
	c.entries.Put(key, cachedPlacement{placement: placement, refusal: refusal, expires: c.now().Add(c.ttl)})
}

func (c *placementCache) forget(key placementKey) {
	c.entries.Remove(key)
}
