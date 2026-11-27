package migrations

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/runtime"
)

// participantExternalIDVersion adds workflow_users.external_id: a person's id
// in the customer's own application, which a directory import matches on.
const participantExternalIDVersion = 64

// participantExternalIDDDL adds the column and its unique constraint, as the
// model declares them. The column is nullable with no default, so adding it is
// a catalog change; the constraint builds an index over a column that is NULL
// on every existing row, and PostgreSQL treats NULLs as distinct, so no
// existing pair of rows can violate it.
const participantExternalIDDDL = `
ALTER TABLE "workflow_users" ADD COLUMN IF NOT EXISTS "external_id" varchar(255);
DO $$ BEGIN
IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'uq_workflow_users_project_id_external_id'
               AND conrelid = 'workflow_users'::regclass) THEN
ALTER TABLE "workflow_users" ADD CONSTRAINT "uq_workflow_users_project_id_external_id" UNIQUE ("project_id", "external_id");
END IF;
END $$;
`

func addParticipantExternalID(ctx context.Context, ex runtime.Executor) error {
	if _, err := ex.Exec(ctx, participantExternalIDDDL, nil); err != nil {
		return fmt.Errorf("add workflow_users.external_id: %w", err)
	}
	return nil
}
