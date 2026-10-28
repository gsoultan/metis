package bpmn_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	serviceimpl "github.com/gsoultan/metis/server/domains/services/impl"
	"github.com/gsoultan/metis/server/repositories/store/deviationrequest"
	"github.com/gsoultan/metis/server/repositories/store/instancedeviation"
	"github.com/gsoultan/storm/runtime"
)

// saidOn is how a sentence writes a moment: the layout the service uses.
const saidOn = "2 January 2006 15:04 MST"

// letTimePass moves a request's deadline into the past, by so many minutes:
// the state time passing creates.
func letTimePass(t *testing.T, h engineHarness, requestID uuid.UUID, minutes int) {
	t.Helper()
	h.rewriteRequest(t, requestID, func(m *deviationrequest.Mut) {
		m.SetExpiresAt(time.Now().Add(-time.Duration(minutes) * time.Minute))
	})
}

// storedRequest is a request as its table holds it, whatever the clock says.
type storedRequest struct {
	Status    string
	LiveKey   *string
	DecidedBy *string
	DecidedAt *time.Time
	ExpiresAt time.Time
}

func requestAsStored(t *testing.T, h engineHarness, requestID uuid.UUID) storedRequest {
	t.Helper()
	row := h.requestRow(t, requestID)
	return storedRequest{
		Status: row.Status, LiveKey: row.LiveKey.Ptr(), DecidedBy: row.DecidedBy.Ptr(),
		DecidedAt: row.DecidedAt.Ptr(), ExpiresAt: row.ExpiresAt,
	}
}

// storedLedgerRow is the ledger row of a request as its table holds it.
type storedLedgerRow struct {
	Status       string
	LiveVisitKey *string
	DecidedAt    *time.Time
}

func ledgerRowAsStored(t *testing.T, h engineHarness, requestID uuid.UUID) storedLedgerRow {
	t.Helper()
	row, found, err := instancedeviation.New().RequestIDEq(requestID).One(h.Ctx(), h.ex())
	if err != nil || !found {
		t.Fatalf("read the ledger row as it is stored: %v (found=%v)", err, found)
	}
	return storedLedgerRow{Status: row.Status, LiveVisitKey: row.LiveVisitKey.Ptr(), DecidedAt: row.DecidedAt.Ptr()}
}

// sweep runs the expiry as the server does: as system work, at a time the
// caller gives.
func (w waiver) sweep(ctx context.Context, now time.Time) (int64, error) {
	swept, err := w.approvals.SweepDeviationRequests(entities.WithSystemContext(ctx), now)
	return swept.Closed(), err
}

// withPatientSweep is the waiver with a sweep that waits for a held row for as
// long as a test may hold it. The server's sweep gives a request up after two
// seconds; a test that stops a sweep on a row of its own, to put two decisions
// in an order, must not have that order depend on how fast the machine is.
// The server's own wait has a test of its own.
func (w waiver) withPatientSweep() waiver {
	w.approvals = serviceimpl.NewDeviationRequestService(w.h.repo, w.h.engine, serviceimpl.WithSweepLockWait(lockWait))
	w.svc = secondedByBudi{asking: w.asking, approvals: w.approvals}
	return w
}

// sendSweep runs the expiry on a goroutine of its own, giving up after lockWait.
func (w waiver) sendSweep(now time.Time) *sent[int64] {
	return send(func() (int64, error) {
		inTime, stop := context.WithTimeout(w.h.Ctx(), lockWait)
		defer stop()
		return w.sweep(inTime, now)
	})
}

// sendRejection has an administrator reject a request on a goroutine of its
// own, giving up after lockWait.
func (w waiver) sendRejection(name string, requestID uuid.UUID) *sent[entities.DeviationRequest] {
	return send(func() (entities.DeviationRequest, error) {
		inTime, stop := context.WithTimeout(w.as(name), lockWait)
		defer stop()
		return w.approvals.RejectDeviationRequest(inTime, requestID, "not needed after all")
	})
}

// holdingTheLedgerRow holds a ledger row and changes nothing on it. Whoever
// closes a request takes the request's row and then this one, so it is where
// a closing can be stopped while it holds its request.
func (h engineHarness) holdingTheLedgerRow(t *testing.T, deviationID uuid.UUID) *heldRows {
	t.Helper()
	return h.holding(t, func(ctx context.Context, tx runtime.Executor) (int, error) {
		return taken(instancedeviation.New().IDEq(deviationID).ForNoKeyUpdate().All(ctx, tx, nil))
	})
}

// holdingTheRequest holds a request's row FOR UPDATE, as a decision holds it.
func (h engineHarness) holdingTheRequest(t *testing.T, requestID uuid.UUID) *heldRows {
	t.Helper()
	return h.holding(t, func(ctx context.Context, tx runtime.Executor) (int, error) {
		return taken(deviationrequest.New().IDEq(requestID).ForUpdate().All(ctx, tx, nil))
	})
}

// plainFailure reports whether err is the server's own failure: an error with
// none of the classes a caller is answered 400, 403 or 404 for.
func plainFailure(err error) bool {
	return err != nil && !errors.Is(err, apierr.ErrInvalidArgument) && !errors.Is(err, apierr.ErrForbidden) && !errors.Is(err, apierr.ErrNotFound)
}
