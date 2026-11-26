package migrations

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/runtime"
)

// machineJoinsVersion adds the machines being added with a one-time code from
// the Servers page.
const machineJoinsVersion = 63

// machineJoinsDDL is the table as the model declares it in this release. It
// is new and referenced by nothing, so creating it locks nothing anybody is
// using.
const machineJoinsDDL = `
CREATE TABLE IF NOT EXISTS "machine_joins" (
    "id" uuid DEFAULT gen_random_uuid() NOT NULL,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL,
    "code_hash" varchar(64) NOT NULL,
    "role" varchar(16) NOT NULL,
    "created_by" varchar(255) NOT NULL,
    "expires_at" timestamptz NOT NULL,
    "name" varchar(255) NOT NULL,
    "machine_key" varchar(64) NOT NULL,
    "words" varchar(64) NOT NULL,
    "requested_at" timestamptz,
    "accepted_by" varchar(255) NOT NULL,
    "accepted_at" timestamptz,
    "sealed" varchar(16384) NOT NULL,
    "collected_at" timestamptz,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "ux_machine_joins_code_hash" ON "machine_joins" ("code_hash");
CREATE INDEX IF NOT EXISTS "ix_machine_joins_expires_at" ON "machine_joins" ("expires_at");
`

func addMachineJoins(ctx context.Context, ex runtime.Executor) error {
	if _, err := ex.Exec(ctx, machineJoinsDDL, nil); err != nil {
		return fmt.Errorf("create the machine_joins table: %w", err)
	}
	return nil
}
