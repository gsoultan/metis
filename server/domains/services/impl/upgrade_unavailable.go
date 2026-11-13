package impl

import (
	"context"
	"errors"

	"github.com/gsoultan/metis/server/domains/services/contracts"
)

var errNoUpgrades = errors.New("upgrades are unavailable: this server was wired without them")

type unavailableUpgrades struct{}

// NewUnavailableUpgradeService returns a service that refuses every call, for
// a facade built without the real one.
func NewUnavailableUpgradeService() contracts.UpgradeService { return unavailableUpgrades{} }

func (unavailableUpgrades) UpgradeStatus(context.Context) (contracts.UpgradeStatus, error) {
	return contracts.UpgradeStatus{}, errNoUpgrades
}

func (unavailableUpgrades) CheckForUpgrade(context.Context) (contracts.UpgradeStatus, error) {
	return contracts.UpgradeStatus{}, errNoUpgrades
}
