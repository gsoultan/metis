package migrations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

const (
	// lockWait is how long to wait for another replica to finish migrating
	// before giving up. Long enough for a real migration, short enough that a
	// deployment fails rather than hangs.
	lockWait = 5 * time.Minute

	// lockPollMin and lockPollMax bound the retry backoff.
	//
	// Most migrations finish in milliseconds, so a replica that loses the race
	// should look again almost immediately rather than sitting out a fixed
	// interval and adding that to every deployment's startup. Backing off keeps
	// a genuinely long migration from being polled thousands of times.
	lockPollMin = 50 * time.Millisecond
	lockPollMax = 2 * time.Second

	// lockStale is when a held lock is assumed to belong to a process that died
	// mid-migration. Taking it over is the lesser risk: without this, one
	// crashed replica blocks every future deployment until someone deletes the
	// row by hand.
	lockStale = 15 * time.Minute
)

// ErrLockTimeout is returned when another process held the migration lock for
// longer than lockWait.
var ErrLockTimeout = errors.New("timed out waiting for the schema migration lock")

// Result reports what a run did, so a deployment log can show it.
type Result struct {
	Applied []int
	Skipped int
}

// Run applies every migration not yet recorded, in version order.
//
// It is safe to call from several replicas at once: they contend for a lock row
// and only one applies anything.
func Run(ctx context.Context, db *gorm.DB, migrations []Migration) (Result, error) {
	if err := ensureBookkeeping(ctx, db); err != nil {
		return Result{}, err
	}

	if err := acquireLock(ctx, db); err != nil {
		return Result{}, err
	}
	defer releaseLock(db)

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return Result{}, err
	}

	ordered := slices.Clone(migrations)
	slices.SortFunc(ordered, func(a, b Migration) int { return a.Version - b.Version })
	if err := assertNoDuplicateVersions(ordered); err != nil {
		return Result{}, err
	}

	var result Result
	for _, migration := range ordered {
		if slices.Contains(applied, migration.Version) {
			result.Skipped++
			continue
		}
		if err := apply(ctx, db, migration); err != nil {
			// Stop at the first failure. Running later migrations against a
			// schema the failed one was supposed to produce turns one broken
			// migration into an unrecoverable database.
			return result, fmt.Errorf("migration %d (%s): %w", migration.Version, migration.Name, err)
		}
		result.Applied = append(result.Applied, migration.Version)
	}
	return result, nil
}

// ensureBookkeeping creates the two tables the runner needs before it can take
// a lock or read what has been applied.
//
// This is the one step that cannot itself be protected by the lock, so replicas
// starting together reach it at the same moment and one of them loses the CREATE
// TABLE race. AutoMigrate is not concurrency-safe and surfaces that as "table
// already exists", which is not a failure — it is the other replica having done
// the job. Treat it as success only after confirming the table is really there,
// so a genuine failure still stops the deployment.
func ensureBookkeeping(ctx context.Context, db *gorm.DB) error {
	scoped := db.WithContext(ctx)

	for _, model := range []any{&SchemaMigration{}, &schemaLock{}} {
		if scoped.Migrator().HasTable(model) {
			continue
		}
		if err := scoped.AutoMigrate(model); err != nil {
			if !scoped.Migrator().HasTable(model) {
				return fmt.Errorf("create migration bookkeeping tables: %w", err)
			}
			log.Debug().Msg("Another replica created the migration bookkeeping tables first")
		}
	}
	return nil
}

// apply runs one migration and records it only once it has succeeded.
func apply(ctx context.Context, db *gorm.DB, migration Migration) error {
	log.Info().Int("version", migration.Version).Str("name", migration.Name).Msg("Applying migration")
	start := time.Now()

	run := func(tx *gorm.DB) error {
		if err := migration.Run(ctx, tx); err != nil {
			return err
		}
		return tx.WithContext(ctx).Create(&SchemaMigration{
			Version:    migration.Version,
			Name:       migration.Name,
			DurationMS: time.Since(start).Milliseconds(),
		}).Error
	}

	var err error
	if migration.Transactional {
		err = db.WithContext(ctx).Transaction(run)
	} else {
		// Recorded in the same statement order, just without the transaction:
		// on MySQL and SQL Server the DDL would have committed regardless, and
		// pretending otherwise would misrepresent how recoverable this is.
		err = run(db)
	}
	if err != nil {
		return err
	}

	log.Info().
		Int("version", migration.Version).
		Dur("took", time.Since(start)).
		Msg("Migration applied")
	return nil
}

// appliedVersions reads what this database has already run.
func appliedVersions(ctx context.Context, db *gorm.DB) ([]int, error) {
	var versions []int
	if err := db.WithContext(ctx).
		Model(&SchemaMigration{}).
		Order("version").
		Pluck("version", &versions).Error; err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	return versions, nil
}

// assertNoDuplicateVersions catches the merge accident where two branches both
// add the same version. Left undetected, whichever ran first would permanently
// mask the other.
func assertNoDuplicateVersions(ordered []Migration) error {
	for i := 1; i < len(ordered); i++ {
		if ordered[i].Version == ordered[i-1].Version {
			return fmt.Errorf("two migrations share version %d (%q and %q); versions are identities and cannot be reused",
				ordered[i].Version, ordered[i-1].Name, ordered[i].Name)
		}
	}
	return nil
}

// acquireLock blocks until this process owns the migration lock.
func acquireLock(ctx context.Context, db *gorm.DB) error {
	owner := lockOwner()
	deadline := time.Now().Add(lockWait)

	// A failed insert is not distinguishable from contention without matching
	// driver-specific duplicate-key errors across four engines, and inspecting
	// the table afterwards does not settle it either: a holder that finishes
	// quickly releases the row between the failed insert and the look, so "no
	// lock is held" is equally consistent with having just missed one.
	//
	// So every failure is treated as contention and retried, and the last one is
	// kept to report if the deadline passes. A genuinely broken database fails
	// the same way, just after the wait rather than immediately.
	var lastErr error
	backoff := lockPollMin

	for {
		err := db.WithContext(ctx).Create(&schemaLock{
			ID:         lockRowID,
			Owner:      owner,
			AcquiredAt: time.Now(),
		}).Error
		if err == nil {
			return nil
		}
		lastErr = err

		if err := clearStaleLock(ctx, db); err != nil {
			return err
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("%w after %s: %w", ErrLockTimeout, lockWait, lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < lockPollMax {
			backoff = min(backoff*2, lockPollMax)
			if backoff == lockPollMax {
				log.Info().Msg("Another replica is still migrating; waiting")
			}
		}
	}
}

// clearStaleLock removes a lock old enough that its owner is presumed dead.
func clearStaleLock(ctx context.Context, db *gorm.DB) error {
	var existing schemaLock
	if err := db.WithContext(ctx).First(&existing, lockRowID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return fmt.Errorf("read migration lock: %w", err)
	}

	if time.Since(existing.AcquiredAt) < lockStale {
		return nil
	}

	log.Warn().
		Str("owner", existing.Owner).
		Time("acquiredAt", existing.AcquiredAt).
		Msg("Taking over a stale migration lock; its holder appears to have died mid-migration")
	return db.WithContext(ctx).Delete(&schemaLock{}, lockRowID).Error
}

// releaseLock drops the lock row. It takes a background context on purpose: the
// caller's may already be cancelled, and a lock left behind blocks the next
// deployment.
func releaseLock(db *gorm.DB) {
	if err := db.Delete(&schemaLock{}, lockRowID).Error; err != nil {
		log.Error().Err(err).Msg("Could not release the migration lock; the next deployment will wait for it to go stale")
	}
}

// lockOwner names the holder well enough to debug a stuck deployment.
func lockOwner() string {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s/%d", host, os.Getpid())
}
