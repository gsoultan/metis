package impl

import (
	"context"
	"errors"

	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
)

// errNoServerSettings is what every method of a facade built without the
// server settings answers.
var errNoServerSettings = errors.New("server settings are unavailable: this server was wired without them")

// unavailableServerSettings is the Null Object for a facade built without the
// server settings service.
type unavailableServerSettings struct{}

// NewUnavailableInstallationSettingService returns a service that refuses
// every call, for a facade built without the real one.
func NewUnavailableInstallationSettingService() contracts.InstallationSettingService {
	return unavailableServerSettings{}
}

func (unavailableServerSettings) ServerSettings(context.Context) (contracts.ServerSettings, error) {
	return contracts.ServerSettings{}, errNoServerSettings
}

func (unavailableServerSettings) ChangeServerSetting(context.Context, contracts.ServerSettingChange) (contracts.ServerSetting, error) {
	return contracts.ServerSetting{}, errNoServerSettings
}

func (unavailableServerSettings) ServerSettingHistory(context.Context, string) ([]entities.InstallationSetting, error) {
	return nil, errNoServerSettings
}
