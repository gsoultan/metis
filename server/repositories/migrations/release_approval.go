package migrations

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/runtime"
)

// releaseApprovalVersion adds who imported a release and who approved it,
// which a protected runtime requires to be two people before it goes live.
const releaseApprovalVersion = 54

// releaseApprovalDDL is the two columns as the model declared them in this
// release: no default, so the default only fills the rows already there. A
// release already imported has neither, and in a protected runtime is
// approved before it goes live, as a new one is.
const releaseApprovalDDL = `
ALTER TABLE "releases" ADD COLUMN IF NOT EXISTS "imported_by" varchar(255) DEFAULT '' NOT NULL;
ALTER TABLE "releases" ALTER COLUMN "imported_by" DROP DEFAULT;
ALTER TABLE "releases" ADD COLUMN IF NOT EXISTS "approved_by" varchar(255) DEFAULT '' NOT NULL;
ALTER TABLE "releases" ALTER COLUMN "approved_by" DROP DEFAULT;
`

func addReleaseApproval(ctx context.Context, ex runtime.Executor) error {
	if _, err := ex.Exec(ctx, releaseApprovalDDL, nil); err != nil {
		return fmt.Errorf("add releases.imported_by and approved_by: %w", err)
	}
	return nil
}
