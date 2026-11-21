package migrations

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/runtime"
)

// releaseDeliveriesVersion adds the releases sent to the servers this one
// gives releases to, and what each of them last said about them.
const releaseDeliveriesVersion = 61

// releaseDeliveriesDDL is the table as the model declares it in this release.
// It is new and referenced by nothing, so creating it locks nothing anybody is
// using.
const releaseDeliveriesDDL = `
CREATE TABLE IF NOT EXISTS "release_deliveries" (
    "id" uuid DEFAULT gen_random_uuid() NOT NULL,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL,
    "connection_id" varchar(36) NOT NULL,
    "project_id" varchar(36) NOT NULL,
    "release" varchar(255) NOT NULL,
    "state" varchar(16) NOT NULL,
    "detail" varchar(1024) NOT NULL,
    "sent_by" varchar(255) NOT NULL,
    "sent_at" timestamptz NOT NULL,
    "collected_at" timestamptz,
    "reported_at" timestamptz,
    PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "ix_release_deliveries_project" ON "release_deliveries" ("project_id");
CREATE UNIQUE INDEX IF NOT EXISTS "ux_release_deliveries_connection_project_release" ON "release_deliveries" ("connection_id", "project_id", "release");
`

func addReleaseDeliveries(ctx context.Context, ex runtime.Executor) error {
	if _, err := ex.Exec(ctx, releaseDeliveriesDDL, nil); err != nil {
		return fmt.Errorf("create the release deliveries table: %w", err)
	}
	return nil
}
