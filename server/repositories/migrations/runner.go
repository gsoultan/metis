package migrations

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store"
	"github.com/gsoultan/metis/server/repositories/store/schemamigration"
	"github.com/gsoultan/storm/migrate"
	"github.com/gsoultan/storm/runtime"
	"github.com/gsoultan/storm/runtime/pgxdrv"
	pgschema "github.com/gsoultan/storm/schema/pg"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// lockWait bounds how long a replica waits for another to finish migrating.
// Long enough for a real migration, short enough that a deployment fails
// rather than hangs.
const lockWait = 5 * time.Minute

// ErrBeforeBaseline is a database that has some of GORM's migrations and not
// all of them.
var ErrBeforeBaseline = errors.New("this database is older than the oldest schema this release can upgrade")

// ErrUnknownDatabase is a database with the product's tables and no migration
// record: not one this product created, or one whose record was removed.
var ErrUnknownDatabase = errors.New("this database has tables but no record of its migrations")

// Result reports what a run did, so a deployment log can show it.
type Result struct {
	// Created is true when the database was empty and has been created from
	// the model.
	Created bool
	// Converted is true when the database was GORM's and has been brought to
	// the model's shape.
	Converted bool
	Applied   []int64
	Skipped   int
}

// Run brings the database the pool reaches up to the last of steps.
//
// An empty database is created from the model and recorded at
// ConvertedVersion. One GORM left at GORMVersion is converted to the model
// first. One recorded at ConvertedVersion or later has every step it has not
// had applied, in version order, each in its own transaction with its record.
// Anything else is refused, and nothing is changed.
//
// Safe from several replicas at once: creating and converting take storm's
// migration lock and diff once they have it, and each step is applied with
// schema_migrations locked and re-read.
func Run(ctx context.Context, pool *pgxpool.Pool, steps []Migration) (Result, error) {
	ordered, err := orderSteps(steps)
	if err != nil {
		return Result{}, err
	}
	namespace := db.Namespace(pool)

	var result Result
	recorded, err := recordedVersions(ctx, pool, namespace)
	if err != nil {
		return Result{}, err
	}
	switch last := maxOf(recorded); {
	case len(recorded) == 0:
		if err := shape(ctx, pool, namespace, nil, createdName); err != nil {
			return Result{}, err
		}
		result.Created = true
	case last < GORMVersion:
		return Result{}, fmt.Errorf("%w: it is at version %d. Upgrade it with the last release that still "+
			"has the migrations up to version %d, start that once so it applies them, then start this one",
			ErrBeforeBaseline, last, GORMVersion)
	case !slices.Contains(recorded, ConvertedVersion):
		if err := shape(ctx, pool, namespace, acceptConversion, convertedName); err != nil {
			return Result{}, fmt.Errorf("convert the database to the storm model's tables: %w", err)
		}
		result.Converted = true
	}

	for _, step := range ordered {
		if slices.Contains(recorded, step.Version) {
			result.Skipped++
			continue
		}
		applied, err := apply(ctx, pool, step)
		if err != nil {
			// Stop at the first failure. Running later steps against a schema
			// the failed one was supposed to produce turns one broken step
			// into an unrecoverable database.
			return result, fmt.Errorf("migration %d (%s): %w", step.Version, step.Name, err)
		}
		if applied {
			result.Applied = append(result.Applied, step.Version)
		} else {
			result.Skipped++
		}
	}
	return result, nil
}

func maxOf(versions []int64) int64 {
	if len(versions) == 0 {
		return 0
	}
	return slices.Max(versions)
}

// orderSteps sorts the steps and refuses a list with two of one version, or
// one at or before ConvertedVersion. Left undetected, whichever ran first would
// permanently mask the other.
func orderSteps(steps []Migration) ([]Migration, error) {
	ordered := slices.Clone(steps)
	slices.SortFunc(ordered, func(a, b Migration) int { return int(a.Version - b.Version) })
	for i, step := range ordered {
		if step.Version <= ConvertedVersion {
			return nil, fmt.Errorf("migration %d (%q) is at or before the storm model's tables, %d; "+
				"a change now is numbered after it", step.Version, step.Name, ConvertedVersion)
		}
		if i > 0 && step.Version == ordered[i-1].Version {
			return nil, fmt.Errorf("two migrations share version %d (%q and %q); versions are identities and cannot be reused",
				step.Version, ordered[i-1].Name, step.Name)
		}
	}
	return ordered, nil
}

// recordedVersions reads the versions the database records, or none for a
// database that holds none of the product's tables. Tables of somebody else's
// are not the product's business and are left alone; one of the product's
// tables with no record of how it got there is refused.
func recordedVersions(ctx context.Context, pool *pgxpool.Pool, namespace string) ([]int64, error) {
	tables, err := tableNames(ctx, pool, namespace)
	if err != nil {
		return nil, err
	}
	if slices.Contains(tables, schemamigration.Table) {
		return readVersions(ctx, pgxdrv.Pool{P: pool})
	}
	want, err := Target()
	if err != nil {
		return nil, err
	}
	for _, table := range tables {
		if want.Table(table) != nil {
			return nil, fmt.Errorf("%w (%s, in %s)", ErrUnknownDatabase, table, namespace)
		}
	}
	return nil, nil
}

func readVersions(ctx context.Context, ex runtime.Executor) ([]int64, error) {
	rows, err := schemamigration.New().All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	versions := make([]int64, 0, len(rows))
	for _, row := range rows {
		versions = append(versions, row.Version)
	}
	return versions, nil
}

// tableNames lists the tables in namespace.
func tableNames(ctx context.Context, pool *pgxpool.Pool, namespace string) ([]string, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("borrow a connection to read the schema: %w", err)
	}
	defer conn.Release()
	live, err := pgschema.Introspect(ctx, conn.Conn(), namespace)
	if err != nil {
		return nil, fmt.Errorf("read the schema of %s: %w", namespace, err)
	}
	names := make([]string, 0, len(live.Tables))
	for _, table := range live.Tables {
		names = append(names, table.Name)
	}
	return names, nil
}

// shape brings the database to the model's tables and records it at
// ConvertedVersion under name.
//
// Through storm's migrate.Auto, which takes a lock and diffs once it has it,
// so a replica that waited behind another finds nothing left to do, and which
// applies the whole plan in one transaction or none of it. A step that can
// lose data is refused unless accept lets it through, and creating an empty
// database has none. Tables the model does not declare are left as they are.
func shape(ctx context.Context, pool *pgxpool.Pool, namespace string, accept func(migrate.Change) bool, name string) error {
	want, err := Target()
	if err != nil {
		return err
	}
	shaped, err := migrate.AutoPool(ctx, pool, want, migrate.AutoOptions{
		Schema:       namespace,
		LockWait:     lockWait,
		DeclaredOnly: true,
		Accept:       accept,
	})
	if err != nil {
		return err
	}
	if !shaped.Empty() {
		log.Info().Int("steps", len(shaped.Applied)).Str("schema", namespace).Str("how", name).
			Msg("Brought the database to the storm model's tables")
	}
	record := schemamigration.Create()
	record.SetVersion(ConvertedVersion)
	record.SetName(name)
	record.SetAppliedAt(time.Now().UTC())
	record.SetDurationMs(0)
	// Another replica that did it at the same moment recorded it too.
	record.DoNothing()
	if _, err := record.Insert(ctx, pgxdrv.Pool{P: pool}); err != nil &&
		!errors.Is(err, runtime.ErrConflict) && !errors.Is(err, runtime.ErrUniqueViolation) {
		return fmt.Errorf("record it: %w", err)
	}
	return nil
}

// acceptConversion lets through what converting GORM's tables takes, and
// nothing that drops data the model still describes: GORM's indexes and
// foreign keys, replaced under the model's names; a column made NOT NULL,
// which fails the whole conversion on a row still NULL where the model
// declares no fill; and text narrowed to the model's width, which fails the
// same way on a value too long for it rather than cutting it. A table or a
// column no longer in the model is refused.
func acceptConversion(c migrate.Change) bool {
	switch c.Object {
	case migrate.ObjectIndex, migrate.ObjectForeignKey, migrate.ObjectUnique, migrate.ObjectCheck:
		return true
	case migrate.ObjectColumn:
		return strings.HasPrefix(c.Why, "making ") || strings.HasPrefix(c.Why, "narrowing ")
	}
	return false
}

// apply runs one step and records it, in one transaction, with
// schema_migrations locked. It reports false when another replica applied the
// step while this one waited for the lock.
func apply(ctx context.Context, pool *pgxpool.Pool, step Migration) (bool, error) {
	start := time.Now()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			log.Warn().Err(err).Int64("version", step.Version).Msg("Could not roll back a migration")
		}
	}()
	ex := pgxdrv.Tx{T: tx}
	if err := store.LockTimeout(ctx, ex, lockWait); err != nil {
		return false, err
	}
	if err := store.LockTables(ctx, ex, store.LockExclusive, store.TableSchemaMigration); err != nil {
		return false, fmt.Errorf("take the migration lock: %w", err)
	}
	recorded, err := readVersions(ctx, ex)
	if err != nil {
		return false, err
	}
	if slices.Contains(recorded, step.Version) {
		return false, nil
	}

	log.Info().Int64("version", step.Version).Str("name", step.Name).Msg("Applying migration")
	if err := step.Run(ctx, ex); err != nil {
		return false, err
	}
	record := schemamigration.Create()
	record.SetVersion(step.Version)
	record.SetName(step.Name)
	record.SetAppliedAt(time.Now().UTC())
	record.SetDurationMs(time.Since(start).Milliseconds())
	if _, err := record.Insert(ctx, ex); err != nil {
		return false, fmt.Errorf("record it: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	log.Info().Int64("version", step.Version).Dur("took", time.Since(start)).Msg("Migration applied")
	return true, nil
}
