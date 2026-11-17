package migrations

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/runtime"
)

// integrationAPIKeysVersion adds integration_api_keys, the keys other systems
// call the API with on behalf of an organization.
const integrationAPIKeysVersion = 58

// integrationAPIKeysDDL is the table as the model declares it in this release.
//
// The foreign key takes a lock on organizations, which most requests read. It
// is bounded, so a long transaction holding organizations fails the upgrade
// with a clear error instead of queueing requests behind the migration.
const integrationAPIKeysDDL = `
DO $$ BEGIN
SET LOCAL lock_timeout = '2s';
IF to_regclass('integration_api_keys') IS NULL THEN
CREATE TABLE "integration_api_keys" (
    "id" uuid DEFAULT gen_random_uuid() NOT NULL,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL,
    "organization_id" uuid NOT NULL,
    "name" varchar(100) NOT NULL,
    "prefix" varchar(16) NOT NULL,
    "token_hash" varchar(64) NOT NULL,
    "roles" jsonb DEFAULT '[]'::jsonb NOT NULL,
    "created_by" varchar(255) NOT NULL,
    "expires_at" timestamptz,
    "last_used_at" timestamptz,
    "revoked_at" timestamptz,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX "ux_integration_api_keys_token_hash" ON "integration_api_keys" ("token_hash");
CREATE INDEX "ix_integration_api_keys_organization_id" ON "integration_api_keys" ("organization_id");
ALTER TABLE "integration_api_keys" ADD CONSTRAINT "fk_integration_api_keys_organization_id" FOREIGN KEY ("organization_id") REFERENCES "organizations" ("id") ON DELETE CASCADE;
END IF;
END $$;
`

// addIntegrationAPIKeys creates the table on a database that does not have it.
func addIntegrationAPIKeys(ctx context.Context, ex runtime.Executor) error {
	if _, err := ex.Exec(ctx, integrationAPIKeysDDL, nil); err != nil {
		return fmt.Errorf("create integration_api_keys: %w", err)
	}
	return nil
}
