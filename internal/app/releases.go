package app

import (
	"github.com/gsoultan/metis/server/domains/services"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// UseReleaseKeys has releases signed and trusted by the keys the embedding
// program keeps. See engine.WithReleaseKeys.
func (a *App) UseReleaseKeys(keys serviceimpl.ReleaseKeys) {
	a.releaseKeys = keys
}

// UseReleasePolicy has the embedding program say which releases need another
// environment's live note. See engine.WithReleasePolicy.
func (a *App) UseReleasePolicy(policy serviceimpl.ReleasePolicy) {
	a.releasePolicy = policy
}

// releaseOptions hands the embedding program's release keys and policy to the
// services, when it gave them. Without, keys come from the environment and no
// release needs a live note.
func (a *App) releaseOptions() []services.FacadeOption {
	var options []services.FacadeOption
	if a.releaseKeys != nil {
		options = append(options, services.WithReleaseKeys(a.releaseKeys))
	}
	if a.releasePolicy != nil {
		options = append(options, services.WithReleasePolicy(a.releasePolicy))
	}
	return options
}
