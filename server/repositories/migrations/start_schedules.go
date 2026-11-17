package migrations

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/runtime"
)

// startSchedulesVersion adds start_schedules, where each process's scheduled
// starts have got to.
const startSchedulesVersion = 56

// startSchedulesDDL is the table as the model declared it in this release.
const startSchedulesDDL = `
DO $$ BEGIN
IF to_regclass('start_schedules') IS NULL THEN
CREATE TABLE "start_schedules" (
    "id" uuid DEFAULT gen_random_uuid() NOT NULL,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL,
    "project_id" uuid NOT NULL,
    "process_key" varchar(191) NOT NULL,
    "checked_until" timestamptz NOT NULL,
    "next_run_at" timestamptz,
    "failures" bigint DEFAULT 0 NOT NULL,
    "last_error" text,
    PRIMARY KEY ("id"),
    CONSTRAINT "uq_start_schedules_process_key_project_id" UNIQUE ("process_key", "project_id")
);
CREATE INDEX "ix_start_schedules_next_run_at" ON "start_schedules" ("next_run_at");
CREATE INDEX "ix_start_schedules_project_id" ON "start_schedules" ("project_id");
ALTER TABLE "start_schedules" ADD CONSTRAINT "fk_start_schedules_project_id" FOREIGN KEY ("project_id") REFERENCES "projects" ("id");
END IF;
END $$;
`

// addStartSchedules creates the table on a database that does not have it.
func addStartSchedules(ctx context.Context, ex runtime.Executor) error {
	if _, err := ex.Exec(ctx, startSchedulesDDL, nil); err != nil {
		return fmt.Errorf("create start_schedules: %w", err)
	}
	return nil
}
