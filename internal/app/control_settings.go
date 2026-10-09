package app

import (
	"os"

	"github.com/rs/zerolog/log"

	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// retiredSoleAdministratorSwitch is the name the exception to the second
// administrator's approval had while it was a switch for the whole
// installation. Nothing reads it. It is known here only so that an operator
// who sets it is told it does nothing.
const retiredSoleAdministratorSwitch = "METIS_ALLOW_SOLE_ADMINISTRATOR_SELF_APPROVAL"

// maxOrganizationsAnnounced is how many of the organizations named as having
// a sole administrator the startup log lists; its count is of all of them.
const maxOrganizationsAnnounced = 50

// logControlSettings says, once at boot, what this installation's settings
// for the second administrator's approval come to when one of them is not
// what was written — and where the one exception to that approval applies,
// when it applies anywhere.
//
// A request for a second administrator expires at a deadline, and the
// deadline comes from a setting. One that cannot be read, or is outside what
// is allowed, is not refused — the server starts, and requests get a deadline
// — so it is said here, to whoever reads the startup log: a deadline nobody
// chose is not a default to fall back on in silence.
//
// Naming an organization as one whose only administrator may approve their
// own request switches a control off there, so while any is named it is a
// warning at every boot, as a withdrawn rule brought back is
// (logLegacySettings): whoever reads the log is often not whoever set it.
// The line says how many organizations and which, what that permits, what it
// does not prevent, and when to take one off the list. An entry that names
// no organization is ignored — and that is said as well, because whoever
// wrote it believes otherwise.
func logControlSettings() {
	if _, problem := serviceimpl.DeviationApprovalTTL(); problem != "" {
		log.Warn().Str("setting", serviceimpl.EnvDeviationApprovalTTL).Msg(problem)
	}
	named, problems := serviceimpl.SoleAdministratorOrganizations()
	for _, problem := range problems {
		log.Warn().Str("setting", serviceimpl.EnvSoleAdministratorOrganizations).Msg(problem)
	}
	if len(named) > 0 {
		listed := make([]string, 0, min(len(named), maxOrganizationsAnnounced))
		for _, organization := range named[:cap(listed)] {
			listed = append(listed, organization.String())
		}
		log.Warn().
			Str("setting", serviceimpl.EnvSoleAdministratorOrganizations).
			Int("count", len(named)).
			Strs("organizations", listed).
			Msg("In each organization this setting names, an administrator may approve their own request for a second " +
				"administrator while nobody else administers that organization. Each such approval needs a reason and is " +
				"recorded as approved by nobody else. It does not stop an administrator of a named organization who can " +
				"change roles from taking another administrator's role away, approving their own request and giving the " +
				"role back, and a change of roles is not recorded with who made it. Name an organization only while it " +
				"has one administrator, and take it off the list once it has a second.")
	}
	logRetiredControlSettings()
}

// logRetiredControlSettings says that a name which is no longer a setting is
// set. The code base has no other retired setting and so no convention for
// one; this is one warning, naming what is not read and what is.
func logRetiredControlSettings() {
	if os.Getenv(retiredSoleAdministratorSwitch) == "" {
		return
	}
	log.Warn().
		Str("setting", retiredSoleAdministratorSwitch).
		Msg(retiredSoleAdministratorSwitch + " is set, and nothing reads it: it turns nothing on. An organization's only " +
			"administrator may approve their own request only where " + serviceimpl.EnvSoleAdministratorOrganizations +
			" names the organization, by id.")
}
