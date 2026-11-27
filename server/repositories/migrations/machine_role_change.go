package migrations

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/runtime"
)

// machineRoleChangeVersion adds what lets the Servers page switch a machine
// between web and worker: the role asked of it, and whether its operator
// fixed its role where the page cannot change it.
const machineRoleChangeVersion = 65

// machineRoleChangeDDL is the columns as the model declares them. They keep
// their defaults: a machine still on the release before them, during an
// upgrade, writes its heartbeat without them and must not be refused.
const machineRoleChangeDDL = `
ALTER TABLE "machines" ADD COLUMN IF NOT EXISTS "requested_role" varchar(16) DEFAULT '' NOT NULL;
ALTER TABLE "machines" ADD COLUMN IF NOT EXISTS "role_fixed" boolean DEFAULT false NOT NULL;
`

func addMachineRoleChange(ctx context.Context, ex runtime.Executor) error {
	if _, err := ex.Exec(ctx, machineRoleChangeDDL, nil); err != nil {
		return fmt.Errorf("add machines.requested_role and machines.role_fixed: %w", err)
	}
	return nil
}
