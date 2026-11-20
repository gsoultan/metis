package migrations

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/runtime"
)

// serverConnectionsVersion adds the servers releases are taken from and given
// to, the one-time codes they connect with, and the key this installation
// signs its releases with.
const serverConnectionsVersion = 60

// serverConnectionsDDL is the tables as the model declares them in this
// release. Each is new and referenced by nothing, so creating them locks
// nothing anybody is using.
const serverConnectionsDDL = `
CREATE TABLE IF NOT EXISTS "connection_codes" (
    "id" uuid DEFAULT gen_random_uuid() NOT NULL,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL,
    "hash" varchar(64) NOT NULL,
    "created_by" varchar(255) NOT NULL,
    "expires_at" timestamptz NOT NULL,
    "used_at" timestamptz,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "ux_connection_codes_hash" ON "connection_codes" ("hash");

CREATE TABLE IF NOT EXISTS "server_connections" (
    "id" uuid DEFAULT gen_random_uuid() NOT NULL,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL,
    "name" varchar(255) NOT NULL,
    "purpose" varchar(16) NOT NULL,
    "side" varchar(16) NOT NULL,
    "url" varchar(2048) NOT NULL,
    "key" varchar(64) NOT NULL,
    "words" varchar(64) NOT NULL,
    "token" varchar(512) NOT NULL,
    "token_hash" varchar(64) NOT NULL,
    "trust" varchar(512) NOT NULL,
    "requested_by" varchar(255) NOT NULL,
    "accepted_by" varchar(255) NOT NULL,
    "accepted_at" timestamptz,
    "last_seen_at" timestamptz,
    PRIMARY KEY ("id")
);
CREATE UNIQUE INDEX IF NOT EXISTS "ux_server_connections_key_side" ON "server_connections" ("key", "side");

CREATE TABLE IF NOT EXISTS "server_signing_keys" (
    "id" uuid DEFAULT gen_random_uuid() NOT NULL,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL,
    "seed" varchar(512) NOT NULL,
    PRIMARY KEY ("id")
);
`

func addServerConnections(ctx context.Context, ex runtime.Executor) error {
	if _, err := ex.Exec(ctx, serverConnectionsDDL, nil); err != nil {
		return fmt.Errorf("create the server connection tables: %w", err)
	}
	return nil
}
