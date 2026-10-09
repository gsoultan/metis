package app

import (
	"github.com/rs/zerolog/log"

	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// logControlSettings says, once at boot, what this installation's settings
// for the second administrator's approval come to when one of them is not
// what was written — and that the one exception to that approval is on,
// when it is.
//
// A request for a second administrator expires at a deadline, and the
// deadline comes from a setting. One that cannot be read, or is outside what
// is allowed, is not refused — the server starts, and requests get a deadline
// — so it is said here, to whoever reads the startup log: a deadline nobody
// chose is not a default to fall back on in silence.
//
// The setting that lets a sole administrator approve their own request is a
// control switched off for the organizations it applies to, so while it is on
// it is a warning at every boot, as a withdrawn rule brought back is
// (logLegacySettings): whoever reads the log is often not whoever set it.
// Written so that it cannot be read, it is off — and that is said as well,
// because whoever wrote it believes otherwise.
func logControlSettings() {
	if _, problem := serviceimpl.DeviationApprovalTTL(); problem != "" {
		log.Warn().Str("setting", serviceimpl.EnvDeviationApprovalTTL).Msg(problem)
	}
	allowed, problem := serviceimpl.AllowSoleAdministratorSelfApproval()
	if problem != "" {
		log.Warn().Str("setting", serviceimpl.EnvAllowSoleAdministratorSelfApproval).Msg(problem)
	}
	if allowed {
		log.Warn().
			Str("setting", serviceimpl.EnvAllowSoleAdministratorSelfApproval).
			Msg("An administrator may approve their own request for a second administrator when nobody else " +
				"administers the organization, because this setting is on. Each such approval needs a reason and is " +
				"recorded as approved by nobody else. Appoint a second administrator, then turn it off.")
	}
}
