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
)

// saidOn is how a sentence writes a moment: the layout the service uses.
const saidOn = "2 January 2006 15:04 MST"

// letTimePass moves a request's deadline into the past, by so many minutes:
// the state time passing creates.
func letTimePass(t *testing.T, h engineHarness, requestID uuid.UUID, minutes int) {
	t.Helper()
	if err := h.db.Exec(`UPDATE deviation_requests SET expires_at = now() - make_interval(mins => ?) WHERE id = ?`, minutes, requestID).Error; err != nil {
		t.Fatalf("let time pass: %v", err)
	}
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
	var stored storedRequest
	if err := h.db.Raw(`SELECT status, live_key, decided_by, decided_at, expires_at FROM deviation_requests WHERE id = ?`, requestID).
		Scan(&stored).Error; err != nil {
		t.Fatalf("read the request as it is stored: %v", err)
	}
	return stored
}

// storedLedgerRow is the ledger row of a request as its table holds it.
type storedLedgerRow struct {
	Status       string
	LiveVisitKey *string
	DecidedAt    *time.Time
}

func ledgerRowAsStored(t *testing.T, h engineHarness, requestID uuid.UUID) storedLedgerRow {
	t.Helper()
	var stored storedLedgerRow
	if err := h.db.Raw(`SELECT status, live_visit_key, decided_at FROM instance_deviations WHERE request_id = ?`, requestID).
		Scan(&stored).Error; err != nil {
		t.Fatalf("read the ledger row as it is stored: %v", err)
	}
	return stored
}

// sweep runs the expiry as the server does: as system work, at a time the
// caller gives.
func (w waiver) sweep(ctx context.Context, now time.Time) (int64, error) {
	return w.approvals.ExpireDeviationRequests(entities.WithSystemContext(ctx), now)
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
	return h.holding(t, `UPDATE instance_deviations SET status = status WHERE id = ?`, deviationID)
}

// holdingTheRequest holds a request's row FOR UPDATE, as a decision holds it.
func (h engineHarness) holdingTheRequest(t *testing.T, requestID uuid.UUID) *heldRows {
	t.Helper()
	return h.holding(t, `WITH taken AS (SELECT id FROM deviation_requests WHERE id = ? FOR UPDATE)
		UPDATE deviation_requests r SET status = r.status FROM taken WHERE r.id = taken.id`, requestID)
}

// plainFailure reports whether err is the server's own failure: an error with
// none of the classes a caller is answered 400, 403 or 404 for.
func plainFailure(err error) bool {
	return err != nil && !errors.Is(err, apierr.ErrInvalidArgument) && !errors.Is(err, apierr.ErrForbidden) && !errors.Is(err, apierr.ErrNotFound)
}
