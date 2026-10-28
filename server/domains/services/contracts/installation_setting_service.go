package contracts

import (
	"context"

	"github.com/gsoultan/metis/server/domains/entities"
)

// InstallationSettingService reads and changes the server settings shown on
// the Settings page.
//
// Every change is a platform administrator's, records who made it and why,
// and never returns a secret: a secret is reported only as set or not set.
type InstallationSettingService interface {
	// ServerSettings is every setting the server reads: the ones the page may
	// change, with where each value comes from, and the ones it only shows.
	ServerSettings(ctx context.Context) (ServerSettings, error)
	// ChangeServerSetting saves a value for a setting, or with a nil value
	// returns it to the environment's value or the default.
	ChangeServerSetting(ctx context.Context, change ServerSettingChange) (ServerSetting, error)
	// ServerSettingHistory lists one setting's changes, newest first.
	ServerSettingHistory(ctx context.Context, name string) ([]entities.InstallationSetting, error)
}
