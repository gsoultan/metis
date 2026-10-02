package migrations

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
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

// taskDelegationBatch is how many rows one statement of the backfill touches.
// One statement over a large inbox holds its row locks until it ends; this many
// at a time keeps each short enough to interleave with work.
const taskDelegationBatch = 100000

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
	if err := returnOwnerlessDelegations(ctx, db); err != nil {
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

// returnOwnerlessDelegations turns every "delegated" row that has no owner
// into a claim by its assignee, or an unclaimed task when it has none. Only
// rows with no owner are touched, so a delegation made since is left alone.
func returnOwnerlessDelegations(ctx context.Context, db *gorm.DB) error {
	for {
		res := db.WithContext(ctx).Exec(`
			UPDATE tasks
			   SET status = CASE WHEN COALESCE(assignee, '') = '' THEN 'unclaimed' ELSE 'claimed' END
			 WHERE id IN (
			       SELECT id FROM tasks
			        WHERE status = 'delegated' AND COALESCE(owner, '') = ''
			        LIMIT ?)`, taskDelegationBatch)
		if res.Error != nil {
			return fmt.Errorf("return delegations with no owner to their assignee: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return nil
		}
	}
}
