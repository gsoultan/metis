package app

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	observercontracts "github.com/gsoultan/metis/server/domains/observers/contracts"
	"github.com/gsoultan/metis/server/domains/services"

	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
)

// maxOrganizationsAnnounced is how many organizations a line of the startup
// log lists; its count is of all of them.
const maxOrganizationsAnnounced = 50

// controlSettings is what this installation's settings for the second
// administrator's approval come to, read once when the server starts.
//
// Once, so that what the startup log announces and what the services are
// built with cannot differ: both are this value. An operator who reads the
// log knows where the one exception to that approval applies, and nothing the
// environment says afterwards makes the log untrue.
type controlSettings struct {
	// soleOrganizations are the organizations whose only administrator may
	// approve a request they asked for themselves, and soleProblems a
	// sentence for each entry of the list that names none.
	soleOrganizations []uuid.UUID
	soleProblems      []string
	// approvalWindowProblem is what was wrong with the approval window as
	// written, or nothing.
	approvalWindowProblem string
}

// readControlSettings reads the settings. It is the one place the list of
// organizations is read from the environment.
func readControlSettings() controlSettings {
	var settings controlSettings
	_, settings.approvalWindowProblem = serviceimpl.DeviationApprovalTTL()
	settings.soleOrganizations, settings.soleProblems = serviceimpl.SoleAdministratorOrganizations()
	return settings
}

// serviceOptions is what the services are built with of these settings.
func (c controlSettings) serviceOptions() []services.FacadeOption {
	return []services.FacadeOption{services.WithSoleAdministratorOrganizations(c.soleOrganizations)}
}

// announce says, once at boot, what the settings come to when one of them is
// not what was written — and where the one exception to the second
// administrator's approval applies, when it applies anywhere.
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
func (c controlSettings) announce() {
	if c.approvalWindowProblem != "" {
		log.Warn().Str("setting", serviceimpl.EnvDeviationApprovalTTL).Msg(c.approvalWindowProblem)
	}
	for _, problem := range c.soleProblems {
		log.Warn().Str("setting", serviceimpl.EnvSoleAdministratorOrganizations).Msg(problem)
	}
	if len(c.soleOrganizations) == 0 {
		return
	}
	log.Warn().
		Str("setting", serviceimpl.EnvSoleAdministratorOrganizations).
		Int("count", len(c.soleOrganizations)).
		Strs("organizations", listed(c.soleOrganizations)).
		Msg("In each organization this setting names, an administrator may approve their own request for a second " +
			"administrator while nobody else administers that organization. Each such approval needs a reason and is " +
			"recorded as approved by nobody else. It does not stop an administrator of a named organization who can " +
			"change roles from taking another administrator's role away, approving their own request and giving the " +
			"role back; each change of roles is recorded in the server's log, with who made it, and nowhere else. " +
			"Name an organization only while it has one administrator, and take it off the list once it has a second.")
}

// listed is the ids a line of the startup log lists: the first
// maxOrganizationsAnnounced of them.
func listed(organizations []uuid.UUID) []string {
	ids := make([]string, 0, min(len(organizations), maxOrganizationsAnnounced))
	for _, organization := range organizations[:cap(ids)] {
		ids = append(ids, organization.String())
	}
	return ids
}

// organizationLookup answers which of some ids name an organization that
// exists (repocontracts.UserRepository does).
type organizationLookup interface {
	LiveOrganizations(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error)
}

// warnOfOrganizationsThatDoNotExist says which of the organizations named as
// having a sole administrator are no organization of this installation.
//
// Such an id does nothing — no request is ever for it — and whoever wrote it
// believes it does something: it is most likely another installation's, or
// mistyped by a character that still left it an id. It cannot be told when
// the settings are announced, before there is a database; it is told here,
// once the repository is up. A failure to ask is said and is nothing more:
// the server starts either way, and the list means what it meant.
func (c controlSettings) warnOfOrganizationsThatDoNotExist(ctx context.Context, lookup organizationLookup) {
	if len(c.soleOrganizations) == 0 {
		return
	}
	live, err := lookup.LiveOrganizations(ctx, c.soleOrganizations)
	if err != nil {
		log.Warn().Err(err).Str("setting", serviceimpl.EnvSoleAdministratorOrganizations).
			Msg("Could not check that the ids this setting lists name organizations of this installation.")
		return
	}
	var unknown []uuid.UUID
	for _, organization := range c.soleOrganizations {
		if !slices.Contains(live, organization) {
			unknown = append(unknown, organization)
		}
	}
	if len(unknown) == 0 {
		return
	}
	log.Warn().
		Str("setting", serviceimpl.EnvSoleAdministratorOrganizations).
		Int("count", len(unknown)).
		Strs("organizations", listed(unknown)).
		Msg("These ids name no organization of this installation, so naming them does nothing. Check them against the " +
			"organizations' ids and take them off the list.")
}

// newServices puts the services together with what this server read of its
// settings when it started.
func (a *App) newServices(dispatcher observercontracts.EventDispatcher, jwtSecret string) services.ServiceFacade {
	return services.NewServiceFacade(a.repo, dispatcher, a.sse, jwtSecret,
		a.participantService(), a.participantSyncService(serviceimpl.NewNoOpLocker()), a.platformUserService(),
		a.control.serviceOptions()...)
}
