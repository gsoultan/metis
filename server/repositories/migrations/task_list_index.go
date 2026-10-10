package migrations

import (
	"context"

	"gorm.io/gorm"
)

// TaskListIndexMigration is the version the index the task list reads
// through is recorded under, named once so the test that rewinds it and the
// list that runs it agree on the row they mean.
const TaskListIndexMigration = 35

// taskListIndex is migration 35: an index in the order the task list pages.
//
// Every page of tasks — a project's, an organization's, the inbox's before its
// own filters — asks for project_id IN (the caller's projects), newest first
// by created_at and then id. The table's indexes are single columns, project_id
// among them, which finds a project's tasks but not in that order: the
// planner reads every task of the project and sorts them to hand back
// twenty-five, so the page people open most gets slower with every task ever
// created.
//
// The same shape as migration 20 gave process_instances, for the same reason:
// project_id leads because tenant scoping is never absent, and the order the
// list asks for follows, id included so the order is total and the index can
// return a page already sorted — a walk of its offset and its length rather
// than a sort of the project. That needs project_id to be an equality, which
// is how the repository asks for one project's tasks. A list across several
// projects is a list of any length to the planner, and still reads their
// tasks and sorts them.
//
// Built CONCURRENTLY, as 20: a task is created and updated in the transaction
// that moves its instance, and a plain build's lock on the table would hold
// up every one of those for as long as the build took.
func taskListIndex() Migration {
	return Migration{
		Version: TaskListIndexMigration,
		Name:    "page a project's tasks from an index",
		Run: func(ctx context.Context, db *gorm.DB) error {
			return createIndexConcurrently(ctx, db,
				"tasks", "ix_tasks_project_created", "project_id, created_at DESC, id DESC")
		},
	}
}
