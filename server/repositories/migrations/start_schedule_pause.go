package migrations

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/runtime"
)

// startSchedulePauseVersion adds start_schedules.paused_at, which says a
// process's scheduled starts are paused and since when.
const startSchedulePauseVersion = 58

// addStartSchedulePause adds the column to a database that does not have it.
// Nullable with no default, so it is a catalog change and rewrites nothing.
func addStartSchedulePause(ctx context.Context, ex runtime.Executor) error {
	const ddl = `ALTER TABLE "start_schedules" ADD COLUMN IF NOT EXISTS "paused_at" timestamptz`
	if _, err := ex.Exec(ctx, ddl, nil); err != nil {
		return fmt.Errorf("add start_schedules.paused_at: %w", err)
	}
	return nil
}
