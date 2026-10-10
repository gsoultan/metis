package impl

import (
	"maps"

	"github.com/gsoultan/metis/server/domains/entities"
	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"

	"github.com/google/uuid"
)

// withApproval is the ledger rows of a migration as an approved run writes
// them: each names the request, who asked — by name and by account — who
// approved, and when. With no approval the rows are answered as they are: a
// cancel, a hold or a mapping that needed nobody else writes what it always
// wrote.
//
// The actor is whoever asked for the migration, whoever's call is running
// it: an approved run executes in the approver's context, and the ledger
// would otherwise take the row for the server's own (no account id), or name
// the approver as having asked.
//
// A run approved by its own requester says so on every row, in the three
// fields a waive approved that way carries: that no second person approved
// it, that the organization was found to have no other administrator, and
// which organization that was. Nothing is written for an approval somebody
// else gave — no key, never false.
//
// The rows handed in are not changed.
func withApproval(rows []entities.Deviation, approval servicecontracts.MigrationApproval) []entities.Deviation {
	if !approval.Granted() {
		return rows
	}
	approved := make([]entities.Deviation, 0, len(rows))
	for _, row := range rows {
		decidedAt := approval.DecidedAt
		row.RequestID = approval.RequestID
		row.Actor, row.ActorID = approval.RequestedBy, approval.RequestedByID
		row.ApprovedBy, row.ApprovedByID, row.DecidedAt = approval.ApprovedBy, approval.ApprovedByID, &decidedAt
		if approval.SelfApproved {
			details := maps.Clone(row.Details)
			if details == nil {
				details = map[string]any{}
			}
			maps.Copy(details, selfApprovalRecord(decisionOf(approval)))
			row.Details = details
		}
		approved = append(approved, row)
	}
	return approved
}

// approvedEntry is a trail entry of a migration as an approved run writes
// it: the entry as it always read, then one sentence saying who approved —
// or that nobody else did — and, in its data, the request and the approver.
// With no approval the entry is answered as it is.
//
// A migration's request is for a version, so it has no entry of its own on
// any one instance's trail: this is where each instance the run acted on
// says that the run was approved, by whom, and on which request. A run its
// requester approved carries the same three fields its ledger rows do.
func approvedEntry(entry entities.AuditEntry, approval servicecontracts.MigrationApproval) entities.AuditEntry {
	if !approval.Granted() {
		return entry
	}
	request := entities.DeviationRequest{ID: approval.RequestID, RequestedBy: approval.RequestedBy}
	decision := decisionOf(approval)
	entry = approvalNote(entry, request, decision)
	if approval.SelfApproved {
		maps.Copy(entry.Data, selfApprovalRecord(decision))
	}
	entry.Narrative += approvalSentence(request, decision)
	return entry
}

// decisionOf is the decision a verified approval records.
func decisionOf(approval servicecontracts.MigrationApproval) entities.DeviationDecision {
	return entities.DeviationDecision{
		Decider: approval.ApprovedBy, DeciderID: approval.ApprovedByID, At: approval.DecidedAt,
		SelfApproved: approval.SelfApproved, Organization: approval.Organization,
	}
}

// selfApprovalRecord is what a record says of an approval given by whoever
// asked: that no second person approved it, that nobody else was found to
// administer the organization, and which organization the exception was
// allowed in. Nothing for an approval somebody else gave.
//
// It is the one writer of the three keys, for a waive's records and a
// migration's: the ledger row, the trail entries and the request's own
// outcome all say them through this, so the two kinds cannot come to record
// a self-approval differently. How many other administrators were found is
// the decision's, from the lookup the approval made.
//
// A migration's approval stores it with the request when it is given, and
// every report on the request afterwards carries it forward: the run reads
// it from there, and asks nobody a second time whether anybody else
// administers the organization.
func selfApprovalRecord(decision entities.DeviationDecision) map[string]any {
	if !decision.SelfApproved {
		return nil
	}
	return map[string]any{
		auditSelfApproved:        true,
		auditOtherAdministrators: decision.OtherAdministrators,
		auditOrganizationID:      decision.Organization.String(),
	}
}

// staleFinding is what a request keeps of having been found stale when
// somebody came to approve it: why, what the plan made at that moment
// refused, and who found it. One shape for a waive's request and a
// migration's.
func staleFinding(decision entities.DeviationDecision, why string, refusals []string) map[string]any {
	return map[string]any{"why": why, "refusals": listed(refusals), "attempted_by": decision.Decider}
}

// carriedSelfApproval is the part of a request's stored outcome that says
// what a self-approval rested on, for the report that replaces that outcome
// to keep. Empty for a request somebody else approved.
func carriedSelfApproval(outcome map[string]any) map[string]any {
	carried := map[string]any{}
	for _, key := range []string{auditSelfApproved, auditOtherAdministrators, auditOrganizationID} {
		if value, said := outcome[key]; said {
			carried[key] = value
		}
	}
	return carried
}

// selfApprovalOrganization is the organization a request its requester
// approved says the exception was allowed in, read from what the approval
// stored (selfApprovalRecord): what the record says, not what the request
// that is being answered happens to be for. The nil id when the request says
// none, or says it in a way that is no id.
func selfApprovalOrganization(request entities.DeviationRequest) uuid.UUID {
	said, isText := request.Outcome[auditOrganizationID].(string)
	if !isText {
		return uuid.Nil
	}
	organization, err := uuid.Parse(said)
	if err != nil {
		return uuid.Nil
	}
	return organization
}
