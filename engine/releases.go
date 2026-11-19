package engine

import (
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// ReleaseKeys are the key the engine signs the releases it exports, and the
// live notes it gives, with, and the keys whose releases and notes it trusts.
type ReleaseKeys = serviceimpl.ReleaseKeys

// ReleasePolicy says which releases go live here only with another
// environment's signed note that they went live there.
type ReleasePolicy = serviceimpl.ReleasePolicy

// WithReleaseKeys has the program that embeds the engine keep the release
// keys, instead of METIS_RELEASE_SIGNING_KEY and METIS_RELEASE_TRUSTED_KEYS.
// That program is then answerable for who may change which keys are trusted.
func WithReleaseKeys(keys ReleaseKeys) Option {
	return func(e *Engine) { e.app.UseReleaseKeys(keys) }
}

// WithReleasePolicy has the program that embeds the engine say which releases
// need another environment's live note before they go live here: a
// production with a staging before it requires staging's. Without it, none
// do.
func WithReleasePolicy(policy ReleasePolicy) Option {
	return func(e *Engine) { e.app.UseReleasePolicy(policy) }
}
