package migrations

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/runtime"
)

// environmentProtectedVersion adds environments.protected, which marks a
// runtime where only a releaser or an administrator may change the models.
const environmentProtectedVersion = 52

// environmentProtectedDDL is the column as the model declared it in this
// release. Every environment already saved stays unprotected, so the upgrade
// changes who may deploy where only once somebody marks one.
const environmentProtectedDDL = `ALTER TABLE "environments" ADD COLUMN IF NOT EXISTS "protected" boolean DEFAULT false NOT NULL`

func addEnvironmentProtected(ctx context.Context, ex runtime.Executor) error {
	if _, err := ex.Exec(ctx, environmentProtectedDDL, nil); err != nil {
		return fmt.Errorf("add environments.protected: %w", err)
	}
	return nil
}
