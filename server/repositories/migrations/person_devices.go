package migrations

import (
	"context"
	"fmt"

	"github.com/gsoultan/storm/runtime"
)

// personDevicesVersion adds person_devices, the phones and browsers People
// registered for push notifications.
const personDevicesVersion = 55

// personDevicesDDL is the table as the model declared it in this release.
const personDevicesDDL = `
DO $$ BEGIN
IF to_regclass('person_devices') IS NULL THEN
CREATE TABLE "person_devices" (
    "id" uuid DEFAULT gen_random_uuid() NOT NULL,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL,
    "project_id" uuid NOT NULL,
    "participant_id" uuid NOT NULL,
    "token" varchar(4096) NOT NULL,
    "token_hash" varchar(64) NOT NULL,
    "platform" varchar(16) NOT NULL,
    "label" varchar(100) NOT NULL,
    "last_sent_at" timestamptz,
    PRIMARY KEY ("id")
);
CREATE INDEX "ix_person_devices_participant_id" ON "person_devices" ("participant_id");
CREATE INDEX "ix_person_devices_project_id" ON "person_devices" ("project_id");
CREATE UNIQUE INDEX "ux_person_devices_token_hash" ON "person_devices" ("token_hash");
ALTER TABLE "person_devices" ADD CONSTRAINT "fk_person_devices_participant_id" FOREIGN KEY ("participant_id") REFERENCES "workflow_users" ("id");
ALTER TABLE "person_devices" ADD CONSTRAINT "fk_person_devices_project_id" FOREIGN KEY ("project_id") REFERENCES "projects" ("id");
END IF;
END $$;
`

// addPersonDevices creates the table on a database that does not have it.
func addPersonDevices(ctx context.Context, ex runtime.Executor) error {
	if _, err := ex.Exec(ctx, personDevicesDDL, nil); err != nil {
		return fmt.Errorf("create person_devices: %w", err)
	}
	return nil
}
