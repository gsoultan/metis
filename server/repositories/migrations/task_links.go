package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// TaskLinksMigration is the version under which a person could act on a task
// through a link. 46 is the audit actor's.
const TaskLinksMigration = 47

// taskLinksDDL is frozen text: the table migration 47 made, which must not
// change when the model does. tests/migrations compares it with the storm
// model and the model is the arbiter.
//
// No foreign key to tasks, by the ledger's rule (instanceDeviationsDDL):
// issuing a link must not wait on a row the engine holds. None to
// workflow_users either: the directory is the main database's, and an
// environment's database has only an empty copy of it. The reference to
// projects is plain. The indexes are plain builds: the table has just been
// created and is read by nothing yet.
var taskLinksDDL = []string{
	`CREATE TABLE IF NOT EXISTS task_links (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    project_id uuid NOT NULL,
    task_id uuid NOT NULL,
    participant_id uuid NOT NULL,
    token_hash varchar(64) NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at timestamptz,
    voided_at timestamptz,
    PRIMARY KEY (id),
    CONSTRAINT fk_task_links_project_id FOREIGN KEY (project_id) REFERENCES projects (id)
)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS ux_task_links_token_hash ON task_links (token_hash)`,
	`CREATE INDEX IF NOT EXISTS ix_task_links_task ON task_links (task_id)`,
	`CREATE INDEX IF NOT EXISTS ix_task_links_project_id ON task_links (project_id)`,
}

// taskLinks is migration 47.
//
// A person given work had to sign in to do it, and many of the people a
// process gives work to — a supplier approving a quote, a manager outside
// the team — do it once and never again. A link mailed with the task lets
// them act on that one task, once.
//
// One transaction, waited for no longer than deviationRequestsLockWait, for
// the reason migration 37 gives: the foreign key takes a brief lock on
// projects. PostgreSQL only, as 21 to 46.
func taskLinks() Migration {
	return Migration{
		Version: TaskLinksMigration,
		Name:    "a person can act on a task through a link",
		Run:     addTaskLinks,
	}
}

func addTaskLinks(ctx context.Context, db *gorm.DB) error {
	if db.Name() != "postgres" {
		return nil
	}
	err := inBoundedTransaction(ctx, db, taskLinksDDL...)
	if lockNotAvailable(err) {
		return fmt.Errorf("projects was held for more than %s by a long query or transaction; "+
			"the upgrade stopped rather than hold every writer of it behind it, and will finish when started again once that ends: %w",
			deviationRequestsLockWait, err)
	}
	if err != nil {
		return fmt.Errorf("create the task links: %w", err)
	}
	return nil
}
