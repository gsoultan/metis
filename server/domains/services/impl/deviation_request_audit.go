package impl

import (
	"fmt"
	"maps"
	"time"

	"github.com/gsoultan/metis/server/domains/entities"
)

// Keys under which a trail entry says which request it belongs to and who
// decided it. A migration's entries carry the same ones.
const (
	auditRequestID    = "request_id"
	auditRequestedBy  = "requested_by"
	auditApprovedBy   = "approved_by"
	auditSelfApproved = "self_approved"
	// auditOtherAdministrators is how many other administrators the
	// organization was found to have when a request was approved by whoever
	// asked for it. It is written beside self_approved and only then, and it
	// is always none: a self-approval is admitted only when the count, made
	// under the request's lock, found nobody else. The record says so in a
	// field of its own, so that what the exception rested on can be read off
	// it, not inferred.
	auditOtherAdministrators = "other_administrators"
)

// requestEntry is a trail entry about a request for a second administrator —
// that it was made, approved, rejected, expired or found stale — on the
// instance the request is for. row is the ledger row of the request, which
// names the instance, its project and the step; narrative is the sentence a
// person reads, said by the caller, who knows what happened.
//
// The entry names the request and who asked, and carries no business value:
// what was asked for, and the values with it, are in the request and the
// ledger row, which are sealed, and the trail is not. Its id and the ledger
// row it points at are set by whoever writes it.
func requestEntry(eventType string, request entities.DeviationRequest, row entities.Deviation, narrative string) entities.AuditEntry {
	entry := entities.AuditEntry{
		Type:      eventType,
		Message:   fmt.Sprintf("%s: request %s", eventType, request.ID),
		Narrative: narrative,
		Timestamp: time.Now(),
		Data: map[string]any{
			auditRequestID:   request.ID.String(),
			auditRequestedBy: request.RequestedBy,
			"request_kind":   string(request.Kind),
			"origin":         string(row.Origin),
			"run_id":         row.RunID.String(),
		},
		Project:  row.Project,
		Instance: row.Instance,
		Node:     row.Node,
	}
	if row.Node != nil {
		entry.Data["node_id"] = row.Node.ID
	}
	return entry
}

// approvalNote marks an entry as made on an approved request: which request,
// and who approved it. It adds keys and nothing else — the entry's sentence
// is its writer's, and a waived step's sentence in particular must not gain
// the word "approved", which there would read as the approval the step was
// for.
//
// self_approved is written only when it is so. An entry's data says what is
// the case, as its control mark does: no key, and never false, is what a
// second administrator's approval looks like.
func approvalNote(entry entities.AuditEntry, request entities.DeviationRequest, decision entities.DeviationDecision) entities.AuditEntry {
	// A copy: the entry handed in is its caller's, and its data may be shared.
	data := maps.Clone(entry.Data)
	if data == nil {
		data = map[string]any{}
	}
	data[auditRequestID] = request.ID.String()
	data[auditApprovedBy] = decision.Decider
	if decision.SelfApproved {
		data[auditSelfApproved] = true
	}
	entry.Data = data
	return entry
}

// approvalSentence is the sentence an approval adds to the entries of a
// migration it let through: who the second administrator was, or that there
// was none. It begins with a space, to follow the sentence it is added to.
//
// Nothing adds it to a waived step's entry (approvalNote).
func approvalSentence(request entities.DeviationRequest, decision entities.DeviationDecision) string {
	if decision.SelfApproved {
		return fmt.Sprintf(" No second administrator approved it: %s approved their own request (request %s).", decision.Decider, request.ID)
	}
	return fmt.Sprintf(" A second administrator, %s, approved it (request %s).", decision.Decider, request.ID)
}
