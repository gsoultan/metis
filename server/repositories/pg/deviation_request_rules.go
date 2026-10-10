package pg

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/store/deviationrequest"
)

// What the repository refuses to write, whoever asks.
//
// Every refusal here is a plain error, not apierr.Invalidf: a request and a
// change of its status are made by a service, never typed by a client, so a
// malformed one has to surface as a server error that is logged and alerted
// on, not as a 400 telling the caller about something it did not do. The unit
// of work around it rolls back.

// wellFormedRequest refuses a request nobody could decide or carry out.
//
// Absent means refuse. A request with no fingerprint would hold nothing, so a
// second one for the same thing would be written beside it; one with no
// deadline would read as expired the moment it was made; one that is born at
// any status but waiting was never asked of anybody.
func wellFormedRequest(r entities.DeviationRequest) error {
	switch {
	case r.Project == nil || r.Project.ID == uuid.Nil:
		return errors.New("deviation request: the project it belongs to is not named")
	case !r.Kind.Valid():
		return fmt.Errorf("deviation request: the kind must come from the closed set (got %q)", r.Kind)
	case r.Status != entities.DeviationRequestPending:
		return fmt.Errorf("deviation request: a request is written waiting for approval and decided afterwards (got status %q)", r.Status)
	case strings.TrimSpace(r.RequestedBy) == "" || r.RequestedByID == uuid.Nil:
		return errors.New("deviation request: who asked, and from which account, is not named")
	case r.Fingerprint == "":
		return errors.New("deviation request: it has no fingerprint, so nothing would stop a second request for the same thing")
	case r.ExpiresAt.IsZero():
		return errors.New("deviation request: it has no deadline")
	case r.DecidedBy != "" || r.DecidedByID != uuid.Nil || r.DecisionReason != "" || r.DecidedAt != nil:
		return errors.New("deviation request: a request that waits has not been decided, and this one names a decision")
	}
	return wellFormedForItsKind(r)
}

// wellFormedForItsKind refuses a request that does not name what its kind
// acts on. The columns are nullable because each kind uses its own.
func wellFormedForItsKind(r entities.DeviationRequest) error {
	switch r.Kind {
	case entities.DeviationRequestInstanceWaive:
		if r.Instance == nil || r.Instance.ID == uuid.Nil {
			return errors.New("deviation request: a waive names the instance it is for, and this one names none")
		}
	case entities.DeviationRequestMigration:
		if r.SourceDefinition == nil || r.SourceDefinition.ID == uuid.Nil ||
			r.TargetDefinition == nil || r.TargetDefinition.ID == uuid.Nil {
			return errors.New("deviation request: a migration names the version it moves from and the one it moves to, and this one does not")
		}
	}
	return nil
}

// requestMoves is every move a request's status makes, by its kind, and there
// is no other.
//
// A waive that waits is decided once: applied — approved and done in the one
// change that does it — rejected, expired, or made stale. It is never
// approved without being applied, so it is never approved and never
// interrupted: those are states of a run, and a waive has none.
//
// A migration that waits is approved, rejected, expired, or made stale. It is
// never applied without having been approved: only its run applies it. An
// approved one is reported on — applied, or interrupted. An interrupted one
// can still be reported on: the sweep gives up on a run after an hour, and a
// run that was in fact still going says what it did when it ends; its report
// wins.
//
// Nothing leaves applied, stale, rejected or expired, and nothing ever becomes
// live again: a request that is over stays over, and the same thing is asked
// for afresh.
var requestMoves = map[entities.DeviationRequestKind]map[entities.DeviationRequestStatus][]entities.DeviationRequestStatus{
	entities.DeviationRequestInstanceWaive: {
		entities.DeviationRequestPending: {
			entities.DeviationRequestApplied, entities.DeviationRequestRejected,
			entities.DeviationRequestExpired, entities.DeviationRequestStale,
		},
	},
	entities.DeviationRequestMigration: {
		entities.DeviationRequestPending: {
			entities.DeviationRequestApproved, entities.DeviationRequestRejected,
			entities.DeviationRequestExpired, entities.DeviationRequestStale,
		},
		entities.DeviationRequestApproved:    {entities.DeviationRequestApplied, entities.DeviationRequestInterrupted},
		entities.DeviationRequestInterrupted: {entities.DeviationRequestApplied, entities.DeviationRequestInterrupted},
	},
}

// checkedMove refuses what no request of any kind does: a status outside the
// closed set, a move no kind makes, and a report that names a decision. It
// needs no row, so it is asked before one is read or held.
func checkedMove(from entities.DeviationRequestStatus, change contracts.DeviationRequestChange) error {
	to := change.Status
	if !from.Valid() || !to.Valid() {
		return fmt.Errorf("deviation request: a status moves within the closed set (got %q to %q)", from, to)
	}
	made := false
	for _, moves := range requestMoves {
		made = made || slices.Contains(moves[from], to)
	}
	if !made {
		return fmt.Errorf("deviation request: nothing moves a request from %s to %s", from, to)
	}
	if from != entities.DeviationRequestPending && namesADecision(change) {
		return fmt.Errorf("deviation request: a request that is %s was decided already; a report on it says what the run did, and this one names who decided, when or why", from)
	}
	return nil
}

// checkedMoveOfKind refuses a move that a request of this kind does not make,
// whatever another kind does: a waive written approved, a migration written
// applied without having been approved.
func checkedMoveOfKind(kind entities.DeviationRequestKind, from, to entities.DeviationRequestStatus) error {
	if !slices.Contains(requestMoves[kind][from], to) {
		return fmt.Errorf("deviation request: nothing moves a request of kind %q from %s to %s", kind, from, to)
	}
	return nil
}

// namesADecision reports whether a change says anything of who decided, from
// which account, when or why.
//
// Who approved is written once, by the change that takes a request out of
// waiting. Every move after that is a report — the run's, or the sweep's —
// and a report that could write these fields could turn a self-approval into
// one a second person gave, or move the time the run window is counted from.
func namesADecision(change contracts.DeviationRequestChange) bool {
	return change.DecidedBy != "" || change.DecidedByID != uuid.Nil || change.DecisionReason != "" || !change.DecidedAt.IsZero()
}

// checkedDecision refuses a move that would leave a request approved, applied
// or rejected without saying who decided, from which account, and when.
//
// All three, on the row as it will be: a field the change leaves zero is left
// as stored, and on a waiting request what is stored is nothing. An approval
// with no account id reads as one a second person gave
// (DeviationRequest.SelfApproved compares account ids), and one with no time
// reads as a run that never reported (RunWindowClosed). A run's report on an
// approved request names nobody (checkedMove refuses one that does) — the
// approval did — and passes on what is stored.
//
// An expiry, a stale request and an interruption are nobody's decision: the
// clock, the instance or a run that failed made them. They are not checked,
// which is also what lets the sweep close an approval that has no time.
func checkedDecision(row deviationrequest.Row, change contracts.DeviationRequestChange) error {
	if !decidedByAPerson(change.Status) {
		return nil
	}
	by := cmp.Or(strings.TrimSpace(change.DecidedBy), strings.TrimSpace(valueOr(row.DecidedBy)))
	account := cmp.Or(change.DecidedByID, uuidOr(row.DecidedByID))
	_, dated := row.DecidedAt.Get()
	if by == "" || account == uuid.Nil || (!dated && change.DecidedAt.IsZero()) {
		return fmt.Errorf("deviation request: a request is %s by somebody, from an account, at some time, and this change leaves one of the three unsaid", change.Status)
	}
	return nil
}

// decidedByAPerson reports whether status is one somebody's decision leaves a
// request at: approved (a migration), applied (a waive, approved and done in
// one change; a migration, approved and then run) or rejected.
func decidedByAPerson(status entities.DeviationRequestStatus) bool {
	switch status {
	case entities.DeviationRequestApproved, entities.DeviationRequestApplied, entities.DeviationRequestRejected:
		return true
	}
	return false
}
