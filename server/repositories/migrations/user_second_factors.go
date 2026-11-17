package migrations

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/runtime"
)

// userSecondFactorsVersion adds user_second_factors, the authenticator apps
// staff accounts sign in with after their password.
const userSecondFactorsVersion = 57

// userSecondFactorsDDL is the table as the model declares it in this release.
//
// The foreign key takes a lock on users, which every request's sign-in reads.
// It is bounded, so a long transaction holding users fails the upgrade with a
// clear error instead of queueing every sign-in behind the migration.
const userSecondFactorsDDL = `
DO $$ BEGIN
SET LOCAL lock_timeout = '2s';
IF to_regclass('user_second_factors') IS NULL THEN
CREATE TABLE "user_second_factors" (
    "id" uuid DEFAULT gen_random_uuid() NOT NULL,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL,
    "user_id" uuid NOT NULL,
    "secret" varchar(512) NOT NULL,
    "confirmed_at" timestamptz,
    "last_step" bigint NOT NULL,
    "recovery_codes" jsonb DEFAULT '[]'::jsonb NOT NULL,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "ux_user_second_factors_user_id" ON "user_second_factors" ("user_id");
ALTER TABLE "user_second_factors" ADD CONSTRAINT "fk_user_second_factors_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE;
END IF;
END $$;
`

// addUserSecondFactors creates the table on a database that does not have it.
func addUserSecondFactors(ctx context.Context, ex runtime.Executor) error {
	if _, err := ex.Exec(ctx, userSecondFactorsDDL, nil); err != nil {
		return fmt.Errorf("create user_second_factors: %w", err)
	}
	return nil
}
