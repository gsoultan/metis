package app

import (
	"github.com/rs/zerolog/log"

	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// logLegacySettings says, once at boot, which withdrawn rules this
// installation has asked to keep.
//
// Each brings back a behaviour that was taken out because it was unsafe, so
// each is a warning rather than a line of configuration: whoever reads the
// startup log — often not whoever set it — should see that it is still on and
// what it reopens.
func logLegacySettings() {
	if serviceimpl.AllowUnassignedTaskClaims() {
		log.Warn().
			Str("setting", serviceimpl.EnvAllowUnassignedTaskClaims).
			Msg("Anybody signed in to an organization can claim and complete its tasks that have no assignee " +
				"and no candidates, because this setting is on. Give those steps an assignee or candidates, " +
				"then turn it off.")
	}
	if serviceimpl.AllowUndeclaredTaskVariables() {
		log.Warn().
			Str("setting", serviceimpl.EnvAllowUndeclaredTaskVariables).
			Msg("Completing a task can set any process variable, including ones its form does not declare, " +
				"because this setting is on. The log names each step that does it, once; give those steps' forms " +
				"the fields, then turn it off.")
	}
}
