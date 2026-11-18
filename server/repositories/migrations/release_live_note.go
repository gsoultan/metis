package migrations

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/runtime"
)

// releaseLiveNoteVersion adds releases.live_note: another environment's signed
// note that a release went live there, attached before it goes live here.
const releaseLiveNoteVersion = 59

// releaseLiveNoteDDL is the column as the model declares it: no default, so
// the default only fills the rows already there, none of which has a note.
const releaseLiveNoteDDL = `
ALTER TABLE "releases" ADD COLUMN IF NOT EXISTS "live_note" jsonb DEFAULT 'null'::jsonb NOT NULL;
ALTER TABLE "releases" ALTER COLUMN "live_note" DROP DEFAULT;
`

func addReleaseLiveNote(ctx context.Context, ex runtime.Executor) error {
	if _, err := ex.Exec(ctx, releaseLiveNoteDDL, nil); err != nil {
		return fmt.Errorf("add releases.live_note: %w", err)
	}
	return nil
}
