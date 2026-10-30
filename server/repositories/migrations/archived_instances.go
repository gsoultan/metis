package migrations

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/runtime"
)

// archivedInstancesVersion adds archived_instances, which the retention
// sweep moves finished instances into.
const archivedInstancesVersion = 51

// archivedInstancesDDL is the table as the model declared it in this
// release, written out rather than diffed from the model at run time: a step
// is a fixed change, and the model will move on after it.
const archivedInstancesDDL = `
DO $$ BEGIN
IF to_regclass('archived_instances') IS NULL THEN
CREATE TABLE "archived_instances" (
    "id" uuid DEFAULT gen_random_uuid() NOT NULL,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL,
    "project_id" uuid NOT NULL,
    "definition_id" uuid NOT NULL,
    "process_key" varchar(255) NOT NULL,
    "process_name" varchar(255) NOT NULL,
    "process_version" int4 NOT NULL,
    "business_key" varchar(255),
    "status" varchar(32) NOT NULL,
    "started_at" timestamptz NOT NULL,
    "finished_at" timestamptz NOT NULL,
    "record_store" varchar(32) NOT NULL,
    "record" text NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX "ix_archived_instances_business_key" ON "archived_instances" ("project_id", "business_key");
CREATE INDEX "ix_archived_instances_created_at" ON "archived_instances" ("created_at");
CREATE INDEX "ix_archived_instances_process_finished" ON "archived_instances" ("project_id", "process_key", "finished_at" DESC, "id" DESC);
CREATE INDEX "ix_archived_instances_project_finished" ON "archived_instances" ("project_id", "finished_at" DESC, "id" DESC);
ALTER TABLE "archived_instances" ADD CONSTRAINT "fk_archived_instances_project_id" FOREIGN KEY ("project_id") REFERENCES "projects" ("id");
END IF;
END $$;
`

// addArchivedInstances creates the table on a database that does not have
// it. A database created from this release's model already does, and is
// left as it is.
func addArchivedInstances(ctx context.Context, ex runtime.Executor) error {
	if _, err := ex.Exec(ctx, archivedInstancesDDL, nil); err != nil {
		return fmt.Errorf("create archived_instances: %w", err)
	}
	return nil
}
