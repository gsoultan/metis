package migrations

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// TaskDelegationMigration is the version under which a delegated task came to
// record who it goes back to. 31 is the task's iteration; versions are
// identities, and a number taken twice stops the server booting.
const TaskDelegationMigration = 32

// taskDelegationLockWait is the longest migration 32 waits for tasks.
//
// The ALTER TABLE needs the table to itself for as long as the catalogue
// takes, and while it waits for a reader to finish, PostgreSQL queues every
// later reader and writer of the table behind it. Every inbox reads tasks and
// every completion writes it, so while a canary runs the release's migrations
// beside the stable pods one long transaction that had read the table would
// stop both for as long as it stayed open. The same bound as migrations 28, 30
// and 31, for the same reason.
const taskDelegationLockWait = "2s"

// taskDelegationBatch is how many rows one transaction of the backfill touches:
// the statement that picks them and the statement that updates them. One
// transaction over a large inbox holds its row locks until it ends; this many
// at a time keeps each short enough to interleave with work.
const taskDelegationBatch = 5000

// taskDelegation is migration 32.
//
// Delegating a task set its status to "delegated" and its assignee to the
// delegate, and kept nothing about who had delegated it: there was nobody to
// hand it back to, and the person it had been given to could not get it back.
// A delegated task now has an owner, the delegate hands it back, and only the
// owner completes it.
//
// Three steps, each safe to repeat, so a run that stops part-way finishes when
// it is started again:
//
//  1. Two nullable columns with no default — a change to the catalogue, not a
//     rewrite of the table — waited for no longer than taskDelegationLockWait.
//  2. A row already "delegated" has no owner, and nothing recorded one. It is
//     what it was in effect: a task its assignee holds, and it becomes a claim
//     by them, so nothing waits for a hand-back nobody can make. One with no
//     assignee either goes back to its queue. In batches.
//  3. The index a person's "delegated by you" list reads through, built
//     CONCURRENTLY, as 20 and 29: a plain build would lock tasks against every
//     write for as long as it took.
//
// Not Transactional, because of the third step (31, which only adds a column,
// is); the first step takes a transaction of its own so the bound on the wait
// ends with it. PostgreSQL only, as 21 to 31, and only where the table exists,
// as 31: anywhere else the baseline has just built the table from the current
// model, columns and index included.
func taskDelegation() Migration {
	return Migration{
		Version: TaskDelegationMigration,
		Name:    "a delegated task has an owner it goes back to",
		Run:     giveDelegatedTasksAnOwner,
	}
}

func giveDelegatedTasksAnOwner(ctx context.Context, db *gorm.DB) error {
	if db.Name() != "postgres" || !db.WithContext(ctx).Migrator().HasTable("tasks") {
		return nil
	}
	if err := addTaskDelegationColumns(ctx, db); err != nil {
		return err
	}
	if err := ReturnOwnerlessDelegations(ctx, db); err != nil {
		return err
	}
	return createIndexConcurrently(ctx, db, "tasks", "ix_tasks_owner", "owner")
}

// addTaskDelegationColumns adds tasks.owner and tasks.delegation_state,
// waiting no longer than taskDelegationLockWait for the table. SET LOCAL: the
// limit ends with this transaction, so the connection goes back to the pool as
// it came.
func addTaskDelegationColumns(ctx context.Context, db *gorm.DB) error {
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(fmt.Sprintf("SET LOCAL lock_timeout = '%s'", taskDelegationLockWait)).Error; err != nil {
			return fmt.Errorf("bound the wait for tasks: %w", err)
		}
		return tx.Exec(`ALTER TABLE tasks
			ADD COLUMN IF NOT EXISTS owner varchar(255),
			ADD COLUMN IF NOT EXISTS delegation_state varchar(32)`).Error
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == postgresLockNotAvailable {
		return fmt.Errorf("tasks was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every inbox and every completion behind it, and will finish when started again once that ends: %w",
			taskDelegationLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("give tasks an owner to go back to: %w", err)
	}
	return nil
}

// ReturnOwnerlessDelegations (exported so the test that races it against a
// concurrent change can run it without the ALTER TABLE that precedes it in the
// migration, which would wait for that change instead of racing it) turns every "delegated" row that has no owner
// into a claim by its assignee, or an unclaimed task when it has none. Only
// rows with no owner are touched, so a delegation made since is left alone.
//
// Each batch is a short transaction of its own: it picks up to
// taskDelegationBatch ids, then updates them with the predicates repeated in
// the outer WHERE. Repeated, because PostgreSQL re-checks only the outer
// condition against a row another transaction changed while this one waited
// for it; with the predicates only in a subquery, a task a delegate completed
// meanwhile would be overwritten back to "claimed".
//
// The loop ends when a batch SELECTS no ids, never when it updates none: a
// batch whose rows were all changed by others updates zero rows while matching
// rows may remain. Every id picked either is updated or has been changed so
// that it no longer matches (or still matches and is picked again), so the
// loop converges.
//
// The batch is 5000 rows: large enough that a hundred thousand legacy rows is
// twenty short transactions, small enough that each holds its row locks for
// milliseconds. lock_timeout bounds the wait on any row another transaction
// holds, so a long transaction cannot make a batch sit on the locks it already
// took; the migration is restartable and says so.
func ReturnOwnerlessDelegations(ctx context.Context, db *gorm.DB) error {
	converted := 0
	for {
		var ids []string
		var n int64
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(fmt.Sprintf("SET LOCAL lock_timeout = '%s'", taskDelegationLockWait)).Error; err != nil {
				return fmt.Errorf("bound the wait for a task: %w", err)
			}
			if err := tx.Raw(`SELECT id::text FROM tasks
				WHERE status = 'delegated' AND COALESCE(owner, '') = ''
				LIMIT ?`, taskDelegationBatch).Scan(&ids).Error; err != nil {
				return err
			}
			if len(ids) == 0 {
				return nil
			}
			res := tx.Exec(`
				UPDATE tasks
				   SET status = CASE WHEN COALESCE(assignee, '') = '' THEN 'unclaimed' ELSE 'claimed' END
				 WHERE id IN ?
				   AND status = 'delegated' AND COALESCE(owner, '') = ''`, ids)
			n = res.RowsAffected
			return res.Error
		})
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == postgresLockNotAvailable {
			return fmt.Errorf("a delegated task was held for more than %s by a long transaction; "+
				"the upgrade stopped rather than wait on it with row locks held, and will finish when started again once that ends: %w",
				taskDelegationLockWait, err)
		}
		if err != nil {
			return fmt.Errorf("return delegations with no owner to their assignee: %w", err)
		}
		if len(ids) == 0 {
			log.Info().Int("converted", converted).
				Msg("Delegated tasks that had no owner to go back to were returned to their assignee")
			return nil
		}
		converted += int(n)
	}
}
