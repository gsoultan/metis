package migrations

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/runtime"
)

// releasesVersion adds releases: the sets of process and decision versions
// that are cut in one environment and imported, then made live, in the next.
const releasesVersion = 53

// releasesDDL is the table as the model declared it in this release.
const releasesDDL = `
DO $$ BEGIN
IF to_regclass('releases') IS NULL THEN
CREATE TABLE "releases" (
    "id" uuid DEFAULT gen_random_uuid() NOT NULL,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL,
    "project_id" uuid NOT NULL,
    "name" varchar(255) NOT NULL,
    "hash" varchar(64) NOT NULL,
    "origin" varchar(16) NOT NULL,
    "state" varchar(16) NOT NULL,
    "bundle" jsonb NOT NULL,
    "items" jsonb NOT NULL,
    "cut_by" varchar(255) NOT NULL,
    "acted_by" varchar(255) NOT NULL,
    "live_at" timestamptz,
    "previous" varchar(255) NOT NULL,
    PRIMARY KEY ("id"),
    CONSTRAINT "uq_releases_project_id_name" UNIQUE ("project_id", "name")
);
CREATE INDEX "ix_releases_project_created" ON "releases" ("project_id", "created_at", "id");
ALTER TABLE "releases" ADD CONSTRAINT "fk_releases_project_id" FOREIGN KEY ("project_id") REFERENCES "projects" ("id");
END IF;
END $$;
`

// addReleases creates the table on a database that does not have it.
func addReleases(ctx context.Context, ex runtime.Executor) error {
	if _, err := ex.Exec(ctx, releasesDDL, nil); err != nil {
		return fmt.Errorf("create releases: %w", err)
	}
	return nil
}
