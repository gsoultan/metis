package migrations

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// InstallationSettingsMigration is the version under which a server setting
// could be set from the Settings page. 48 is the personal API keys'.
const InstallationSettingsMigration = 49

// installationSettingsDDL is frozen text: the table migration 49 made, which
// must not change when the model does. tests/migrations compares it with the
// storm model and the model is the arbiter.
//
// No foreign keys: the account that made a change is kept by id, and the
// history outlives it. The index is a plain build: the table has just been
// created and is read by nothing yet.
var installationSettingsDDL = []string{
	`CREATE TABLE IF NOT EXISTS installation_settings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    updated_at timestamptz DEFAULT now() NOT NULL,
    name varchar(100) NOT NULL,
    value text,
    sealed boolean NOT NULL,
    reason text NOT NULL,
    set_by varchar(255) NOT NULL,
    set_by_id uuid NOT NULL,
    PRIMARY KEY (id)
)`,
	`CREATE INDEX IF NOT EXISTS ix_installation_settings_name ON installation_settings (name, created_at, id)`,
}

// installationSettings is migration 49.
//
// Every server setting was an environment variable, so changing the mail
// server or a retention period meant editing the deployment and restarting it.
// An administrator can now set the operational ones from the Settings page;
// each change is a row here, and the environment still wins where it is set.
//
// A new table touches no other, so nothing waits on a lock. PostgreSQL only,
// as 21 to 48, and only on the main database: an environment's database is
// configured by the main one's settings.
func installationSettings() Migration {
	return Migration{
		Version: InstallationSettingsMigration,
		Name:    "server settings can be set from the Settings page",
		Run:     addInstallationSettings,
	}
}

func addInstallationSettings(ctx context.Context, db *gorm.DB) error {
	if db.Name() != "postgres" || !db.WithContext(ctx).Migrator().HasTable("workflow_users") {
		return nil
	}
	if err := inBoundedTransaction(ctx, db, installationSettingsDDL...); err != nil {
		return fmt.Errorf("create the installation settings: %w", err)
	}
	return nil
}
