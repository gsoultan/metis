package impl

import (
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/server/domains/entities"
)

// traceRefusedSelfApproval says in the server's log that whoever asked for a
// request tried to approve it and was refused.
//
// The refusal changes nothing, so the ledger and the trail — which record
// what was done — say nothing of it, and an attempt to get round the second
// administrator would otherwise leave no mark anywhere. The line names the
// request and the account — by its id as well as its name, since the id is
// what the control tells people apart by and a name can be changed — and
// nothing that was asked for: a log is read by more people than a ledger is.
func traceRefusedSelfApproval(request entities.DeviationRequest, caller entities.User) {
	log.Warn().Str("request", request.ID.String()).Str("actor", caller.Username).Str("actor_id", caller.ID.String()).
		Msg("An administrator tried to approve their own request for a second administrator and was refused. " +
			"Nothing changed, and nothing else records the attempt.")
}

// traceSelfApproval says in the server's log that an administrator approved
// their own request, in an organization the installation names as one whose
// only administrator may.
//
// The ledger and the trail record it, and say no second person approved. This
// line is for whoever watches the installation rather than the instance: the
// one exception to the second administrator was used, under which setting,
// in which organization, on which request and by which account — by its id
// as well as its name, since the id is what the control tells people apart
// by. As the line of a refused attempt, it carries nothing that was asked for
// and not the reason given.
//
// request is the request as the approval left it, so the line is written
// only for an approval that was kept.
func traceSelfApproval(request entities.DeviationRequest, organization uuid.UUID) {
	log.Warn().Str("setting", EnvSoleAdministratorOrganizations).Str("organization", organization.String()).
		Str("request", request.ID.String()).Str("actor", request.DecidedBy).Str("actor_id", request.DecidedByID.String()).
		Msg("An administrator approved their own request for a second administrator, which this setting allows in an " +
			"organization it names while nobody else administers it. No second person approved it; the ledger and the trail say so.")
}

// traceNamedOrganizationHasAnotherAdministrator says in the server's log that
// an organization the installation names as having one administrator has
// another.
//
// The requester's own approval was refused, as it is wherever somebody else
// can approve, so nothing went wrong — but the list is out of date, and a
// list left that way is an exception waiting for the day one of the two
// administrators removes the other. Nothing else would tell whoever keeps
// the list. Said each time such a refusal is given, from the count the
// refusal was decided on.
func traceNamedOrganizationHasAnotherAdministrator(request entities.DeviationRequest, organization uuid.UUID) {
	log.Warn().Str("setting", EnvSoleAdministratorOrganizations).Str("organization", organization.String()).
		Str("request", request.ID.String()).
		Msg("This setting names this organization as having one administrator, and it has another: an administrator's " +
			"approval of their own request was refused. Take the organization off the list.")
}

// traceApprovalNotApplied says in the server's log that an approval was given
// and the waive it approved could not be applied.
//
// Everything the approval did is undone and the request waits as it did, so
// that somebody approved it is recorded nowhere else. refused says the
// failure was told to the approver as theirs to act on — a gateway after the
// step had no way out for the values asked for — rather than as the server's.
// When the approval was the requester's own, the line says so: an approval
// by nobody else that came to nothing is still the exception having been
// used. The values, the gateway and the failure's own words stay out of the
// line.
func traceApprovalNotApplied(request entities.DeviationRequest, decision entities.DeviationDecision, refused bool) {
	line := log.Warn().Str("request", request.ID.String()).Str("actor", decision.Decider).Str("actor_id", decision.DeciderID.String())
	what := "An approved waive"
	if decision.SelfApproved {
		line = line.Bool("own_request", true)
		what = "A waive its requester approved themselves"
	}
	if refused {
		line.Msg(what + " could not be applied: what follows the step had no way out for what was asked. " +
			"Nothing changed and the request still waits; it can be rejected and asked for again.")
		return
	}
	line.Msg(what + " could not be applied: it failed for a reason that is the server's. " +
		"Nothing changed and the request still waits.")
}
