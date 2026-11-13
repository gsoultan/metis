package upgrade

import "github.com/gsoultan/metis/server/domains/services/contracts"

// StatusResponse is what the Upgrades page shows.
type StatusResponse struct {
	Status *contracts.UpgradeStatus `json:"status,omitzero"`
	Err    error                    `json:"err,omitzero"`
}

func (r StatusResponse) Failed() error { return r.Err }
