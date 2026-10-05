package app

import (
	"github.com/rs/zerolog/log"

	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// logControlSettings says, once at boot, what this installation's settings
// for the second administrator's approval come to when one of them is not
// what was written.
//
// A request for a second administrator expires at a deadline, and the
// deadline comes from a setting. One that cannot be read, or is outside what
// is allowed, is not refused — the server starts, and requests get a deadline
// — so it is said here, to whoever reads the startup log: a deadline nobody
// chose is not a default to fall back on in silence.
func logControlSettings() {
	if _, problem := serviceimpl.DeviationApprovalTTL(); problem != "" {
		log.Warn().Str("setting", serviceimpl.EnvDeviationApprovalTTL).Msg(problem)
	}
}
