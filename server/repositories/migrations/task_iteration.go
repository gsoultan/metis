package migrations

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// TaskIterationMigration is the version under which a task came to record the
// iteration of its step it was created for. Named once, here, so the test that
// rewinds it and the list that runs it cannot disagree about which row they
// mean.
const TaskIterationMigration = 31

// taskIterationLockWait is the longest migration 31 waits for tasks.
//
// The ALTER TABLE needs the table to itself for as long as the catalogue takes,
// and while it waits for a reader to finish, PostgreSQL queues every later
// reader and writer of the table behind it. The inbox reads tasks on every
// page, so while a canary runs the release's migrations beside the stable pods,
// one long transaction that had read the table would stop every inbox for as
// long as it stayed open. The same bound as migrations 28 and 30, for the same
// reason.
const taskIterationLockWait = "2s"

// taskIteration is migration 31.
//
// A step that runs once per item gives each run a token carrying its
// iteration, and a task row recorded none. Completing a task therefore named no
// iteration, the engine retired no token, and an approval every approver had
// given left its tokens on the step for good: the instance reached its end
// event and never completed.
//
// Nothing is filled in. Which iteration an existing task was created for was
// never recorded, and guessing one per row would be writing a guess into the
// record. A task with no iteration completes by retiring the lowest-numbered
// iteration still waiting on its step — see
// entities.ProcessInstance.WaitingIteration.
//
// Adding a nullable column with no default is a change to the catalogue, not a
// rewrite of the table, so the lock is held for no longer than that takes —
// and it is waited for no longer than taskIterationLockWait.
//
// PostgreSQL only, as 21 to 30. And only where the table exists: a fresh
// installation's baseline has just built tasks from the current model, column
// included.
func taskIteration() Migration {
	return Migration{
		Version: TaskIterationMigration,
		Name:    "a task records which iteration of its step it is",
		// So the bound on the wait ends with the migration: SET LOCAL.
		Transactional: true,
		Run:           addTaskIterationWithoutQueueingTheInbox,
	}
}

// addTaskIterationWithoutQueueingTheInbox adds tasks.iteration_id, waiting no
// longer than taskIterationLockWait for the table. SET LOCAL: the limit ends
// with the migration's transaction, so the connection goes back to the pool as
// it came.
func addTaskIterationWithoutQueueingTheInbox(ctx context.Context, tx *gorm.DB) error {
	if tx.Name() != "postgres" || !tx.WithContext(ctx).Migrator().HasTable("tasks") {
		return nil
	}
	if err := tx.WithContext(ctx).Exec(fmt.Sprintf("SET LOCAL lock_timeout = '%s'", taskIterationLockWait)).Error; err != nil {
		return fmt.Errorf("bound the wait for tasks: %w", err)
	}
	err := tx.WithContext(ctx).Exec(`ALTER TABLE tasks ADD COLUMN IF NOT EXISTS iteration_id text`).Error
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == postgresLockNotAvailable {
		return fmt.Errorf("tasks was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every inbox behind it, and will finish when started again once that ends: %w",
			taskIterationLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("give tasks the iteration they were created for: %w", err)
	}
	return nil
}
