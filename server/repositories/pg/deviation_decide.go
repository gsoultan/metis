package pg

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/instancedeviation"
)

// NewDeviationDecider returns the ledger as what decides a row that waited
// for approval. It is the ledger's own repository behind a second, narrower
// contract: the three test doubles of DeviationRepository keep their three
// methods.
func NewDeviationDecider(c *db.Conn) contracts.DeviationDecider {
	return &deviationRepository{conn{conn: c}}
}

// Decide moves a ledger row from pending_approval to what became of it, once,
// inside the transaction that decides its request.
//
// It takes the row (FOR UPDATE) and only then looks at its status, so a
// decision that waited for another reads what the other left: of two made at
// once, one is written and the other is ErrDeviationRowDecided. Whoever
// decides a row holds its request's row already — request, then instance,
// then task rows, then this — so two decisions of one row meet at the request
// first; the row's own lock is what holds if a caller ever forgets.
//
// A decision the caller got wrong — back to pending_approval, a status
// outside the closed set, no time, applied with nobody having approved — is a
// plain error, as a malformed row is (checkedTarget): it is made by a
// service, never by a client.
func (r *deviationRepository) Decide(ctx context.Context, id uuid.UUID, decision contracts.LedgerRowDecision) (entities.Deviation, error) {
	if !db.InTransaction(ctx) {
		return entities.Deviation{}, contracts.ErrDeviationOutsideTransaction
	}
	if err := wellFormedDecision(decision); err != nil {
		return entities.Deviation{}, err
	}
	row, err := r.waitingRow(ctx, id)
	if err != nil {
		return entities.Deviation{}, err
	}
	if err := approvedBySomebody(decision); err != nil {
		return entities.Deviation{}, err
	}
	mut, err := stageDecision(row, decision)
	if err != nil {
		return entities.Deviation{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return entities.Deviation{}, err
	}
	if err := mut.Update(ctx, ex); err != nil {
		return entities.Deviation{}, fmt.Errorf("could not record the decision of the deviation: %w", err)
	}
	return deviationFrom(mut.Row())
}

// wellFormedDecision refuses a decision that decides nothing, or says nothing
// of when.
func wellFormedDecision(decision contracts.LedgerRowDecision) error {
	if !decision.Status.Valid() || decision.Status == entities.DeviationPendingApproval {
		return fmt.Errorf("deviation: a row that waits is decided to applied, rejected, expired or stale (got %q)", decision.Status)
	}
	if decision.DecidedAt.IsZero() {
		return errors.New("deviation: a decision is recorded with the time it was made, and this one has none")
	}
	return nil
}

// approvedBySomebody refuses a decision to applied that does not say who
// approved, and from which account.
//
// A row that waited is applied because somebody approved it; the request
// beside it is refused the same decision without a decider (checkedDecision),
// and a ledger that said "applied" with nobody named would be the record of
// an approval nobody gave. A row that is rejected, has expired or went stale
// was approved by nobody, and names nobody.
//
// Asked of a row that still waits, after it is held: a row already decided is
// answered as decided, whatever the second decision lacked.
func approvedBySomebody(decision contracts.LedgerRowDecision) error {
	if decision.Status != entities.DeviationApplied {
		return nil
	}
	if strings.TrimSpace(decision.ApprovedBy) == "" || decision.ApprovedByID == uuid.Nil {
		return errors.New("deviation: a row that waited is applied because somebody approved it, and this decision does not say who, or from which account")
	}
	return nil
}

// waitingRow reads a ledger row by id within the caller's organization, holds
// it, and refuses one that does not wait for approval.
//
// The scope is in the statement, so another organization's row is neither read
// nor held.
func (r *deviationRepository) waitingRow(ctx context.Context, id uuid.UUID) (instancedeviation.Row, error) {
	scope, err := r.scopeOf(ctx)
	if err != nil {
		return instancedeviation.Row{}, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return instancedeviation.Row{}, err
	}
	q := instancedeviation.New().Where(instancedeviation.ID.Eq(id))
	if !scope.unrestricted() {
		if len(scope.projects) == 0 {
			return instancedeviation.Row{}, fmt.Errorf("%w: no such deviation", apierr.ErrNotFound)
		}
		q = q.Where(instancedeviation.ProjectID.In(uuidsToRaw(scope.projects)...))
	}
	row, found, err := q.ForUpdate().One(ctx, ex)
	if err != nil {
		return instancedeviation.Row{}, fmt.Errorf("could not read the deviation: %w", err)
	}
	if !found {
		return instancedeviation.Row{}, fmt.Errorf("%w: no such deviation", apierr.ErrNotFound)
	}
	if row.Status != string(entities.DeviationPendingApproval) {
		return instancedeviation.Row{}, contracts.ErrDeviationRowDecided
	}
	return row, nil
}

// stageDecision stages what a decision writes: the status, who and when, the
// live key that follows from the status, and whatever else it names. What it
// leaves zero or nil is not assigned, so it stays as the request wrote it.
func stageDecision(row instancedeviation.Row, decision contracts.LedgerRowDecision) (instancedeviation.Mut, error) {
	mut := instancedeviation.Mutate(row)
	mut.SetStatus(string(decision.Status))
	mut.SetUpdatedAtNow()
	setOrNullString(mut.SetApprovedBy, mut.SetApprovedByNull, decision.ApprovedBy)
	setOrNullUUID(mut.SetApprovedByID, mut.SetApprovedByIDNull, decision.ApprovedByID)
	mut.SetDecidedAt(decision.DecidedAt.UTC())
	// The same rule the insert follows (liveVisitKey), from the new status:
	// the visit key while the row is live — applied here, since a decision is
	// never back to waiting — and NULL once it is rejected, expired or stale.
	// Written, not only cleared: a row that somehow waited with no live key
	// holds its visit from the moment it is applied.
	held := liveVisitKey(entities.Deviation{Status: decision.Status, VisitKey: valueOr(row.VisitKey)})
	setOrNullString(mut.SetLiveVisitKey, mut.SetLiveVisitKeyNull, held)
	if decision.AuditEntryID != uuid.Nil {
		mut.SetAuditEntryID(decision.AuditEntryID)
	}
	if decision.Task != nil && decision.Task.ID != uuid.Nil {
		mut.SetTaskID(decision.Task.ID)
	}
	if err := stageDecidedDocuments(&mut, decision); err != nil {
		return instancedeviation.Mut{}, err
	}
	return mut, nil
}

// stageDecidedDocuments stages the before, after and details a decision
// gives: the values at the moment of the change, sealed as the insert seals
// them. A nil map is left as stored.
func stageDecidedDocuments(mut *instancedeviation.Mut, decision contracts.LedgerRowDecision) error {
	if decision.Before != nil {
		before, err := deviationSealed(decision.Before)
		if err != nil {
			return fmt.Errorf("could not encode the deviation's before: %w", err)
		}
		mut.SetBefore(before)
	}
	if decision.After != nil {
		after, err := deviationSealed(decision.After)
		if err != nil {
			return fmt.Errorf("could not encode the deviation's after: %w", err)
		}
		mut.SetAfter(after)
	}
	if decision.Details != nil {
		details, err := deviationDetails(decision.Details)
		if err != nil {
			return fmt.Errorf("could not encode the deviation's details: %w", err)
		}
		mut.SetDetails(details)
	}
	return nil
}
