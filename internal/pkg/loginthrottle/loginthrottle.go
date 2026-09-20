// Package loginthrottle slows repeated failed sign-ins for one account.
//
// The HTTP rate limiter bounds requests per client address, which stops one
// source hammering the server and does nothing about the same account being
// tried from many. bcrypt's cost makes each guess expensive, but "expensive"
// is a throughput argument and credential stuffing is a patience argument:
// spread across enough addresses, a per-IP limit never fires.
//
// So this counts consecutive failures per *account* and makes each one cost
// more than the last.
//
// # Why backoff and not lockout
//
// A hard lockout after N failures is a denial of service against any username
// an attacker can guess — and usernames are not secret. Locking out `admin`
// costs an attacker nothing and costs the installation its administrator.
// Backoff makes guessing impractical without ever making an account
// unreachable: the legitimate owner waits seconds, an attacker needs years.
//
// # Bounds
//
// The key is attacker-supplied, so the map is an LRU with a fixed capacity and
// no unbounded growth. Eviction is safe in the direction that matters: losing
// an entry forgets failures, which is the same position as a fresh account,
// and an attacker who could force eviction would have to make enough distinct
// sign-in attempts that the per-IP limiter is already answering them.
package loginthrottle

import (
	"sync"
	"time"

	"github.com/gsoultan/metis/internal/pkg/lru"
)

const (
	// FreeAttempts is how many consecutive failures cost nothing.
	//
	// Three, because a person mistyping a password twice and getting it right
	// on the third go should never meet this at all. The delay starts after
	// that, when the pattern stops looking like typing.
	FreeAttempts = 3

	// BaseDelay is the wait after the first failure past FreeAttempts, doubling
	// with each one after.
	BaseDelay = 1 * time.Second

	// MaxDelay caps the doubling. Five minutes is long enough that a guessing
	// campaign is hopeless and short enough that a locked-out person is not
	// filing a ticket.
	MaxDelay = 5 * time.Minute

	// Forget is how long an idle account keeps its failure count. A person who
	// walks away and comes back an hour later starts fresh.
	Forget = 1 * time.Hour

	// Capacity bounds the map. Ten thousand distinct usernames in flight is far
	// past any real installation and small enough to be free.
	Capacity = 10000
)

type state struct {
	failures int
	last     time.Time
}

// Throttle tracks consecutive sign-in failures per account. The zero value is
// not usable; call New.
type Throttle struct {
	mu       sync.Mutex
	accounts *lru.Cache[string, state]
}

func New() *Throttle {
	return &Throttle{accounts: lru.New[string, state](Capacity)}
}

// RetryAfter reports how long this account must wait before another attempt is
// worth making, and whether it must wait at all.
//
// It does not itself record an attempt: a caller checks, and reports the
// outcome afterwards through Failed or Succeeded. Keeping those separate means
// a refused attempt does not extend its own penalty, which would let an
// attacker lock an account out by continuing to knock.
func (t *Throttle) RetryAfter(username string, now time.Time) (time.Duration, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	s, ok := t.accounts.Get(username)
	if !ok || s.failures <= FreeAttempts {
		return 0, false
	}
	if now.Sub(s.last) >= Forget {
		t.accounts.Remove(username)
		return 0, false
	}

	wait := delayFor(s.failures)
	elapsed := now.Sub(s.last)
	if elapsed >= wait {
		return 0, false
	}
	return wait - elapsed, true
}

// Failed records an unsuccessful sign-in.
func (t *Throttle) Failed(username string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	s, ok := t.accounts.Get(username)
	if ok && now.Sub(s.last) >= Forget {
		// Idle long enough to have been forgotten; start again rather than
		// resuming a count from another day.
		s = state{}
	}
	s.failures++
	s.last = now
	t.accounts.Put(username, s)
}

// Succeeded clears the count. A correct password is the end of the sequence,
// whatever came before it — otherwise somebody who mistyped four times would
// keep paying for it after getting in.
func (t *Throttle) Succeeded(username string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.accounts.Remove(username)
}

// delayFor doubles from BaseDelay for each failure past the free ones, to
// MaxDelay. Failures at or below FreeAttempts cost nothing.
func delayFor(failures int) time.Duration {
	over := failures - FreeAttempts
	if over <= 0 {
		return 0
	}
	delay := BaseDelay
	for range over - 1 {
		delay *= 2
		if delay >= MaxDelay {
			return MaxDelay
		}
	}
	return delay
}
