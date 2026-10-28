package impl

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/gsoultan/metis/internal/pkg/clamp"
	"github.com/gsoultan/storm/pglock"
	"github.com/gsoultan/storm/runtime/pgxdrv"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// PostgresLocker is a Strategy implementation of DistributedLocker built on
// PostgreSQL advisory locks, for coordinating work that must have exactly one
// owner across engine replicas.
//
// **It pins a connection per held lock, and that is not an optimisation.**
// Advisory locks are *session*-scoped, while a pgxpool.Pool is a pool: a naive
// implementation acquires on whichever connection the pool hands out and
// releases on whichever it hands out next. When those differ,
// pg_advisory_unlock runs on a session that holds nothing — it returns false
// and raises a warning the driver discards, so the release *reports success*
// while the lock stays held on an idle pooled connection for as long as that
// connection lives. Every other replica then waits on a lock whose owner
// finished long ago. That was the behaviour here until it was reproduced by
// tests/postgres/advisory_lock_test.go.
//
// TTL behaviour: the ttl argument is deliberately ignored. Advisory locks have
// no time-based expiry; a lock lives until it is released or its session ends.
// What that buys is crash safety — a replica that dies loses its connections,
// and Postgres drops the locks with them, so no lock outlives its owner. What
// it does not buy is rescue from a live-but-wedged holder, so callers that need
// that must add a heartbeat or use a TTL-capable backend.
//
// Callers must pair every successful TryAcquire with exactly one Release, or
// the pinned connection is withheld from the pool until the process exits.
type PostgresLocker struct {
	pool *pgxpool.Pool

	// mu guards held. Locks are taken from several worker goroutines, so the
	// map from key to its pinned session needs the same protection the
	// underlying pool already gives itself.
	mu   sync.Mutex
	held map[string]*pgxpool.Conn
}

// NewPostgresLocker creates a new PostgresLocker backed by the given pool.
func NewPostgresLocker(pool *pgxpool.Pool) *PostgresLocker {
	return &PostgresLocker{pool: pool, held: make(map[string]*pgxpool.Conn)}
}

// TryAcquire attempts a non-blocking advisory lock via pg_try_advisory_lock,
// pinning the session that takes it so Release can run on that same session.
// The key is hashed to the 64-bit integer the advisory lock functions take.
func (l *PostgresLocker) TryAcquire(ctx context.Context, key string, _ time.Duration) (bool, error) {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("postgres advisory lock %q: reserve a connection: %w", key, err)
	}

	acquired, err := pglock.TryLock(ctx, pgxdrv.Conn{C: conn}, hashKey(key))
	if err != nil {
		releaseConn(conn, key, "after a failed acquire")
		return false, fmt.Errorf("postgres advisory lock acquire %q: %w", key, err)
	}
	if !acquired {
		// Somebody else holds it. Nothing to keep the session for.
		releaseConn(conn, key, "after losing the lock")
		return false, nil
	}

	l.mu.Lock()
	previous, alreadyHeld := l.held[key]
	l.held[key] = conn
	l.mu.Unlock()

	if alreadyHeld {
		// Defensive: two successful acquires of one key by one locker should be
		// impossible, since the second runs on a different session and Postgres
		// would refuse it. If it ever happens, releasing the session we just
		// displaced is better than leaking it silently — and saying so, because
		// reaching here means an assumption above is wrong.
		log.Warn().Str("key", key).Msg("Displaced an existing advisory lock session for the same key")
		if _, err := pglock.Unlock(ctx, pgxdrv.Conn{C: previous}, hashKey(key)); err != nil {
			log.Warn().Err(err).Str("key", key).Msg("Could not unlock the displaced advisory lock session")
		}
		releaseConn(previous, key, "after displacing a duplicate holder")
	}
	return true, nil
}

// Release releases the advisory lock on the same session that took it, then
// returns that session to the pool.
//
// Releasing a key this locker does not hold is not an error: a caller unwinding
// after a failed acquire should not have to know whether the acquire got far
// enough to matter, and reporting one here would turn a tidy cleanup path into
// a spurious error in the log.
func (l *PostgresLocker) Release(ctx context.Context, key string) error {
	l.mu.Lock()
	conn, held := l.held[key]
	delete(l.held, key)
	l.mu.Unlock()

	if !held {
		return nil
	}

	// Release returns the session to the pool; it does not end it, so the unlock
	// has to be explicit. Closing regardless of the unlock's outcome is
	// deliberate — a session we can no longer unlock on is one we must not keep
	// reserved, and Postgres frees its locks when that session finally ends.
	defer releaseConn(conn, key, "after releasing")

	released, err := pglock.Unlock(ctx, pgxdrv.Conn{C: conn}, hashKey(key))
	if err != nil {
		return fmt.Errorf("postgres advisory lock release %q: %w", key, err)
	}
	if !released {
		// The session held no such lock. With a pinned connection this should
		// be unreachable, and it is precisely the silent failure this type
		// exists to avoid — so it is reported rather than discarded.
		return fmt.Errorf("postgres advisory lock release %q: the session did not hold it", key)
	}
	return nil
}

// releaseConn returns a reserved session to the pool.
//
// A connection that failed mid-statement is closed rather than returned:
// pgxpool checks what it is handed back, so a broken session is not lent out
// again, and Postgres frees whatever locks it held when it ends.
func releaseConn(conn *pgxpool.Conn, key, when string) {
	if conn.Conn().IsClosed() {
		log.Warn().Str("key", key).Str("when", when).Msg("The advisory lock connection was lost; its locks went with it")
	}
	conn.Release()
}

// hashKey converts an arbitrary string key into a stable int64 for advisory lock IDs.
func hashKey(key string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return clamp.Int64FromUint64(h.Sum64())
}
