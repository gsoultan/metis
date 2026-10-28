package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// InstallationSettingRepository keeps the server settings set from the
// Settings page. Each change is a row that is never rewritten, so the history
// is the audit trail.
type InstallationSettingRepository interface {
	// Record adds a change. A secret's value is sealed before it is written.
	Record(ctx context.Context, setting entities.InstallationSetting) (entities.InstallationSetting, error)

	// Current is every setting's newest change, values opened.
	Current(ctx context.Context) ([]entities.InstallationSetting, error)

	// History is one setting's changes, newest first. Secret values are not
	// opened: a history says that a secret changed, never what it was.
	History(ctx context.Context, name string, limit int64) ([]entities.InstallationSetting, error)
}
