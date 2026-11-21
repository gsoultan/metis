package migrations

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/runtime"
)

// machinesVersion adds the processes of this installation, each written by
// the process itself every few seconds while it runs.
const machinesVersion = 62

// machinesDDL is the table as the model declares it in this release. It is
// new and referenced by nothing, so creating it locks nothing anybody is
// using.
const machinesDDL = `
CREATE TABLE IF NOT EXISTS "machines" (
    "id" uuid DEFAULT gen_random_uuid() NOT NULL,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL,
    "name" varchar(255) NOT NULL,
    "address" varchar(255) NOT NULL,
    "role" varchar(16) NOT NULL,
    "version" varchar(64) NOT NULL,
    "started_at" timestamptz NOT NULL,
    "last_seen_at" timestamptz NOT NULL,
    PRIMARY KEY ("id")
);
CREATE INDEX IF NOT EXISTS "ix_machines_last_seen_at" ON "machines" ("last_seen_at");
`

func addMachines(ctx context.Context, ex runtime.Executor) error {
	if _, err := ex.Exec(ctx, machinesDDL, nil); err != nil {
		return fmt.Errorf("create the machines table: %w", err)
	}
	return nil
}
