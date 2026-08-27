package postgres_test

import (
	"database/sql"
	"os"
	"sync"
	"testing"
	"time"

	serviceimpl "github.com/gsoultan/gobpm/server/domains/services/impl"
	"github.com/gsoultan/gobpm/tests/testutils"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// PostgreSQL advisory locks are *session*-scoped, and PostgresLocker is handed a
// connection *pool*. Those two facts do not compose on their own: an acquire and
// its matching release run on whichever connection the pool hands out, and
// pg_advisory_unlock on a session that does not hold the lock does nothing at
// all — it returns false and raises a warning the driver discards.
//
// The consequence is not a failed release, which would at least be visible. It
// is a lock that stays held on an idle pooled connection for as long as that
// connection lives, while the caller is told the release succeeded. Every other
// replica then waits on a lock whose owner has already moved on.
//
// These tests pin the property that matters — release must actually release,
// including when the release lands on a different connection than the acquire.

// openPool returns a raw *sql.DB against the test PostgreSQL, skipping when no
// DSN is configured, like every other test in this package.
func openPool(t *testing.T, maxConns int) *sql.DB {
	t.Helper()
	dsn := os.Getenv(testutils.PostgresDSNEnv)
	if dsn == "" {
		t.Skipf("set %s to run advisory lock tests against a live PostgreSQL", testutils.PostgresDSNEnv)
	}
	gormDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	db, err := gormDB.DB()
	if err != nil {
		t.Fatalf("sql.DB: %v", err)
	}
	db.SetMaxOpenConns(maxConns)
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return db
}

// TestAdvisoryLockReleasesWhenTheReleaseLandsOnAnotherConnection is the
// reproduction. Holding the acquiring connection out of the pool forces the
// release onto a different session, which is the ordinary case under any real
// concurrency — it is only hidden in a single-threaded test because the pool
// hands the same connection straight back.
func TestAdvisoryLockReleasesWhenTheReleaseLandsOnAnotherConnection(t *testing.T) {
	ctx := t.Context()
	const key = "gobpm-test:cross-connection-release"

	owner := serviceimpl.NewPostgresLocker(openPool(t, 4))
	rival := serviceimpl.NewPostgresLocker(openPool(t, 4))

	acquired, err := owner.TryAcquire(ctx, key, time.Minute)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !acquired {
		t.Fatal("could not acquire a fresh lock; a previous run may have leaked it")
	}

	// A second replica must not get it while it is held.
	if held, err := rival.TryAcquire(ctx, key, time.Minute); err != nil {
		t.Fatalf("rival acquire while held: %v", err)
	} else if held {
		t.Fatal("two replicas hold the same lock at once")
	}

	if err := owner.Release(ctx, key); err != nil {
		t.Fatalf("release: %v", err)
	}

	// The whole point: after a release that reported success, the lock must
	// genuinely be free for another replica.
	got, err := rival.TryAcquire(ctx, key, time.Minute)
	if err != nil {
		t.Fatalf("rival acquire after release: %v", err)
	}
	if !got {
		t.Fatal("the lock is still held after Release reported success: the unlock ran on a pooled connection that never held it")
	}
	if err := rival.Release(ctx, key); err != nil {
		t.Fatalf("rival release: %v", err)
	}
}

// TestAdvisoryLockSurvivesPoolChurn runs enough acquire/release cycles across
// concurrent keys that the pool must hand out different connections, which is
// the condition the single-cycle case cannot guarantee on its own.
func TestAdvisoryLockSurvivesPoolChurn(t *testing.T) {
	ctx := t.Context()
	pool := openPool(t, 4)
	locker := serviceimpl.NewPostgresLocker(pool)
	rival := serviceimpl.NewPostgresLocker(openPool(t, 4))

	for i := range 8 {
		key := "gobpm-test:churn"

		acquired, err := locker.TryAcquire(ctx, key, time.Minute)
		if err != nil {
			t.Fatalf("cycle %d acquire: %v", i, err)
		}
		if !acquired {
			t.Fatalf("cycle %d: the previous cycle's release did not free the lock", i)
		}

		// Churn the pool so the release is unlikely to reuse the acquiring
		// connection: hold a few connections open across the release.
		var held []*sql.Conn
		for range 3 {
			c, err := pool.Conn(ctx)
			if err != nil {
				break
			}
			held = append(held, c)
		}

		if err := locker.Release(ctx, key); err != nil {
			t.Fatalf("cycle %d release: %v", i, err)
		}
		for _, c := range held {
			_ = c.Close()
		}

		// An independent pool proves the lock is free at the server, not just
		// re-entrant within the session that took it.
		free, err := rival.TryAcquire(ctx, key, time.Minute)
		if err != nil {
			t.Fatalf("cycle %d rival acquire: %v", i, err)
		}
		if !free {
			t.Fatalf("cycle %d: lock leaked — Release reported success but the lock is still held", i)
		}
		if err := rival.Release(ctx, key); err != nil {
			t.Fatalf("cycle %d rival release: %v", i, err)
		}
	}
}

// TestAdvisoryLockAdmitsExactlyOneHolder is the guarantee the type exists for:
// several replicas racing for the same key, exactly one winner. Run under -race
// it also covers the map of pinned sessions, which is touched from every worker
// goroutine that takes a lock.
func TestAdvisoryLockAdmitsExactlyOneHolder(t *testing.T) {
	ctx := t.Context()
	const key = "gobpm-test:exactly-one"
	const replicas = 6

	lockers := make([]*serviceimpl.PostgresLocker, replicas)
	for i := range lockers {
		// A pool each: two goroutines sharing one pool could be handed the same
		// session, and advisory locks are re-entrant within a session, so that
		// would prove nothing about mutual exclusion between replicas.
		lockers[i] = serviceimpl.NewPostgresLocker(openPool(t, 2))
	}

	start := make(chan struct{})
	results := make(chan bool, replicas)
	var wg sync.WaitGroup
	for _, locker := range lockers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			acquired, err := locker.TryAcquire(ctx, key, time.Minute)
			if err != nil {
				results <- false
				return
			}
			results <- acquired
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	winners := 0
	for acquired := range results {
		if acquired {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("%d replicas hold the lock at once, want exactly 1", winners)
	}

	for _, locker := range lockers {
		if err := locker.Release(ctx, key); err != nil {
			t.Fatalf("release: %v", err)
		}
	}

	// And the lock is genuinely free afterwards, not merely forgotten locally.
	after := serviceimpl.NewPostgresLocker(openPool(t, 2))
	free, err := after.TryAcquire(ctx, key, time.Minute)
	if err != nil {
		t.Fatalf("acquire after all releases: %v", err)
	}
	if !free {
		t.Fatal("the lock outlived every holder's release")
	}
	if err := after.Release(ctx, key); err != nil {
		t.Fatalf("final release: %v", err)
	}
}
