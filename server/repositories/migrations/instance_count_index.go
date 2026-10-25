package migrations

import (
	"context"

	"gorm.io/gorm"
)

// InstanceCountIndexMigration is the version the index the instance counts
// read through is recorded under, named once so the test that rewinds it and
// the list that runs it agree on the row they mean.
const InstanceCountIndexMigration = 47

// instanceCountIndex is migration 47: an index the instance counts are
// answered from without visiting the table.
//
// Every page of the instance list asks how many of the project's instances are
// in each state, and the version history asks the same per version. Both group
// a project's instances that are not deleted by status, and definition_id too
// for the history. ix_process_instances_project_status from migration 20 leads
// with the right columns, but deleted_at is not in it, so every entry it
// returns is a visit to the row — and a row carries the instance's variables
// and tokens. On 200,000 instances, 50,000 of them in one project, the planner
// preferred to read the whole table: 28,000 pages for one count.
//
// (project_id, definition_id, status) over the rows that are not deleted holds
// every column those reads look at, so they are index-only scans: 45 pages
// instead, and about 4 ms rather than 120. definition_id comes before status
// so a count of one version is a range of the index, not a filter of the
// project's entries. The condition is the one every read of this table writes
// into its statement rather than binding, so a generic plan can still prove
// it, as migration 29 explains.
//
// The columns are ones the table already indexes, so a write that leaves them
// alone stays a HOT update, as it is today.
//
// Built CONCURRENTLY, as 20: an instance is written by every step it takes,
// and a plain build's lock on the table would hold every one of them up for as
// long as the build took.
func instanceCountIndex() Migration {
	return Migration{
		Version: InstanceCountIndexMigration,
		Name:    "count a project's instances from an index",
		Run: func(ctx context.Context, db *gorm.DB) error {
			return createPartialIndexConcurrently(ctx, db,
				"process_instances", "ix_process_instances_live_counts",
				"project_id, definition_id, status", "deleted_at IS NULL")
		},
	}
}
