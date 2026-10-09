package impl

import (
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/server/domains/entities"
)

// traceRefusedSelfApproval says in the server's log that whoever asked for a
// request tried to approve it and was refused.
//
// The refusal changes nothing, so the ledger and the trail — which record
// what was done — say nothing of it, and an attempt to get round the second
// administrator would otherwise leave no mark anywhere. The line names the
// request and the account and nothing that was asked for: a log is read by
// more people than a ledger is.
func traceRefusedSelfApproval(request entities.DeviationRequest, caller entities.User) {
	log.Warn().Str("request", request.ID.String()).Str("actor", caller.Username).
		Msg("An administrator tried to approve their own request for a second administrator and was refused. " +
			"Nothing changed, and nothing else records the attempt.")
}

// traceApprovalNotApplied says in the server's log that a second
// administrator approved a waive and it could not be applied.
//
// Everything the approval did is undone and the request waits as it did, so
// that somebody approved it is recorded nowhere else. refused says the
// failure was told to the approver as theirs to act on — a gateway after the
// step had no way out for the values asked for — rather than as the server's.
// The values, the gateway and the failure's own words stay out of the line.
func traceApprovalNotApplied(request entities.DeviationRequest, decision entities.DeviationDecision, refused bool) {
	line := log.Warn().Str("request", request.ID.String()).Str("actor", decision.Decider)
	if refused {
		line.Msg("An approved waive could not be applied: what follows the step had no way out for what was asked. " +
			"Nothing changed and the request still waits; it can be rejected and asked for again.")
		return
	}
	line.Msg("An approved waive could not be applied: it failed for a reason that is the server's. " +
		"Nothing changed and the request still waits.")
}
