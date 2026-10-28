package contracts

import (
	"time"

	"github.com/gsoultan/metis/internal/pkg/settings"
)

// SettingSource says where the value a setting has comes from.
type SettingSource string

const (
	// SourceEnvironment is the server's environment, which wins over the page:
	// the page shows the setting and cannot change it.
	SourceEnvironment SettingSource = "environment"
	// SourceSaved is a value saved on the Settings page.
	SourceSaved SettingSource = "saved"
	// SourceDefault is nothing set anywhere: the server's own default.
	SourceDefault SettingSource = "default"
)

// ServerSettings is the Settings page's view of the server.
type ServerSettings struct {
	Settings        []ServerSetting
	EnvironmentOnly []EnvironmentSetting
}

// ServerSetting is one setting the page may change.
//
// Value is empty for a secret, whatever is set: IsSet says whether one is.
type ServerSetting struct {
	Definition settings.Definition
	Source     SettingSource
	Value      string
	IsSet      bool
	// PendingRestart is a saved value this replica does not run with yet,
	// because the setting is read when the server starts.
	PendingRestart bool
	UpdatedAt      *time.Time
	UpdatedBy      string
}

// EnvironmentSetting is a setting only the environment sets.
type EnvironmentSetting struct {
	Fixed settings.Fixed
	IsSet bool
	// Value is empty for a secret.
	Value string
}

// ServerSettingChange is a change asked for on the page.
type ServerSettingChange struct {
	Name   string
	Value  *string
	Reason string
}
