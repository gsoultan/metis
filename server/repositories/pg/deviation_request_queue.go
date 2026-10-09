package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
	"github.com/gsoultan/metis/server/repositories/store/deviationrequest"
)

// maxSweepBatch bounds what one sweep read holds, whatever it is asked for:
// each row it answers is read whole, and locked until the transaction ends.
const maxSweepBatch = 1000

// List answers one page of the caller's requests, newest first, and how many
// match in all.
//
// Scoped by project, as every list is: the caller's projects, narrowed to the
// one the query names. Another organization's project lists nothing.
//
// The page is the queue's projection, not the row: no command, no plan and no
// list of instances. The list has no bound and the other two are sealed, so
// read with every row a page would cost what its largest requests weigh, on
// demand, for anyone who may list. The page itself is bounded
// (contracts.MaxPageSize), and the total is a count, which no limit cuts.
//
// The order is total — creation time, then id — because requests written in
// one transaction share a creation time, and among ties the database may
// answer a different order for each page.
func (r *deviationRequestRepository) List(ctx context.Context, query entities.DeviationRequestQuery, now time.Time) ([]entities.DeviationRequest, int64, error) {
	none := []entities.DeviationRequest{}
	if query.Status != "" && !query.Status.Valid() {
		return none, 0, fmt.Errorf("deviation request: a list is by a status of the closed set (got %q)", query.Status)
	}
	var requested uuid.UUID
	if query.Project != nil {
		requested = query.Project.ID
	}
	projects, visible, err := r.scopedProjects(ctx, requested)
	if err != nil || !visible {
		return none, 0, err
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return none, 0, err
	}
	q := readingAs(deviationrequest.New(), query.Status, now)
	if projects != nil {
		q = q.Where(deviationrequest.ProjectID.In(uuidsToRaw(projects)...))
	}
	total, err := q.Count(ctx, ex)
	if err != nil {
		return none, 0, fmt.Errorf("could not count the requests: %w", err)
	}
	page := contracts.Pagination{Page: query.Page, PageSize: query.PageSize}
	rows, err := q.
		Order(deviationrequest.CreatedAt.Desc(), deviationrequest.ID.Desc()).
		Limit(int64(page.Normalize().PageSize)).
		Offset(int64(page.Offset())).
		AllQueue(ctx, ex)
	if err != nil {
		return none, 0, fmt.Errorf("could not list the requests: %w", err)
	}
	out := make([]entities.DeviationRequest, 0, len(rows))
	for _, row := range rows {
		request, err := queuedRequestFrom(row)
		if err != nil {
			return none, 0, err
		}
		out = append(out, request)
	}
	return out, total, nil
}

// readingAs narrows q to the requests that read as status at now.
//
// This is DeviationRequest.EffectiveStatus written as a predicate, and has to
// agree with it for every row: a request listed under a status it does not
// read as is offered for a decision nobody can make, and one listed under
// none is lost. The clock closes a live request before any sweep writes it
// down — a waiting one at its deadline, an approved one at its deadline, an
// hour after the approval, or at once when the approval has no time — so
// "expired" and "interrupted" each take the rows stored that way and the rows
// the clock has made so, and "pending_approval" and "approved" leave those
// out.
//
// In each Any the comparisons come first and IsNull last. An earlier version
// of storm dropped the predicates that followed a null test in a disjunction;
// at the version in use it does not, and the order is kept as a guard — it
// costs nothing, and the test of every shape of row would catch a predicate
// that went missing.
func readingAs(q deviationrequest.Query, status entities.DeviationRequestStatus, now time.Time) deviationrequest.Query {
	var (
		pending     = string(entities.DeviationRequestPending)
		approved    = string(entities.DeviationRequestApproved)
		expired     = string(entities.DeviationRequestExpired)
		interrupted = string(entities.DeviationRequestInterrupted)
		reportBy    = now.Add(-entities.ApprovedRunReportWindow)
	)
	switch status {
	case "":
		return q
	case entities.DeviationRequestPending:
		return q.Where(deviationrequest.Status.Eq(pending), deviationrequest.ExpiresAt.Gt(now))
	case entities.DeviationRequestExpired:
		return q.Where(deviationrequest.Status.In(expired, pending)).
			Any(deviationrequest.Status.Eq(expired), deviationrequest.ExpiresAt.Lte(now))
	case entities.DeviationRequestApproved:
		return q.Where(deviationrequest.Status.Eq(approved),
			deviationrequest.ExpiresAt.Gt(now), deviationrequest.DecidedAt.Gt(reportBy))
	case entities.DeviationRequestInterrupted:
		return q.Where(deviationrequest.Status.In(interrupted, approved)).
			Any(deviationrequest.Status.Eq(interrupted), deviationrequest.ExpiresAt.Lte(now),
				deviationrequest.DecidedAt.Lte(reportBy), deviationrequest.DecidedAt.IsNull())
	}
	return q.Where(deviationrequest.Status.Eq(string(status)))
}

// ListOverdue answers the requests that still wait at or after their
// deadline, locked, the longest overdue first, strictly after the cursor.
func (r *deviationRequestRepository) ListOverdue(ctx context.Context, now time.Time, after contracts.SweepCursor, limit int) ([]entities.DeviationRequest, error) {
	return r.sweep(ctx, after, limit, deviationrequest.New().Where(
		deviationrequest.Status.Eq(string(entities.DeviationRequestPending)),
		deviationrequest.ExpiresAt.Lte(now)))
}

// ListUnreported answers the approved requests whose run window has closed,
// locked, strictly after the cursor: DeviationRequest.RunWindowClosed as a
// predicate. An approval with no time is among them — its window cannot be
// said to be open — so the sweep closes it at once rather than at its
// deadline. The null test comes last, as a guard (see readingAs).
func (r *deviationRequestRepository) ListUnreported(ctx context.Context, now time.Time, after contracts.SweepCursor, limit int) ([]entities.DeviationRequest, error) {
	return r.sweep(ctx, after, limit, deviationrequest.New().
		Where(deviationrequest.Status.Eq(string(entities.DeviationRequestApproved))).
		Any(deviationrequest.ExpiresAt.Lte(now),
			deviationrequest.DecidedAt.Lte(now.Add(-entities.ApprovedRunReportWindow)),
			deviationrequest.DecidedAt.IsNull()))
}

// BoundLockWait bounds, for the rest of the caller's transaction, how long a
// statement waits for a row lock somebody else holds.
//
// The setting is local to the transaction (SET LOCAL), so it ends with it and
// touches no other user of the connection. It takes no parameter the server
// can bind, so the wait is written into the statement — as a whole number of
// milliseconds, which is all a duration can become.
func (r *deviationRequestRepository) BoundLockWait(ctx context.Context, wait time.Duration) error {
	if !db.InTransaction(ctx) {
		return contracts.ErrDeviationRequestOutsideTransaction
	}
	if wait < time.Millisecond {
		return fmt.Errorf("deviation request: a sweep bounds its wait for a lock at a millisecond or more (got %s)", wait)
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return err
	}
	if _, err := ex.Exec(ctx, fmt.Sprintf("SET LOCAL lock_timeout = %d", wait.Milliseconds()), nil); err != nil {
		return fmt.Errorf("could not bound the sweep's wait for a lock: %w", err)
	}
	return nil
}

// sweep reads up to limit of the rows q matches that come after the cursor,
// and holds them, FOR UPDATE SKIP LOCKED, until the transaction ends.
//
// SKIP LOCKED because a row somebody holds is being decided or reported on
// right now, and what they write wins: the sweep passes it by instead of
// waiting to write over it. It is a request row and nothing is locked before
// it here, so the order — request, then instance, then task rows — holds for
// whatever the caller does next. A row read here is moved with Transition,
// which writes only while the stored status is still the one read.
//
// After the cursor, in the order of the read — deadline, then id — as one
// row comparison, so the index is walked from where the last read stopped.
// It is what carries a pass beyond a request it could not close: such a
// request stays where it is in the order, and read from the start it would be
// answered first every time.
//
// The rows are answered as the queue answers them — no command, no plan, no
// list of instances — though the statement reads them whole, because a read
// that locks cannot be the projection (its statement carries no lock). The
// sweep has no use for the three, and opening them here would let one request
// whose sealed plan no longer opens fail the read it is in.
//
// No project is joined and no deleted project is left out: run as system
// work this reaches every request there is, which it has to — a request
// whose project was deleted still has a ledger row holding its visit. Run for
// one organization it reads only that organization's.
func (r *deviationRequestRepository) sweep(ctx context.Context, after contracts.SweepCursor, limit int, q deviationrequest.Query) ([]entities.DeviationRequest, error) {
	if !db.InTransaction(ctx) {
		return nil, contracts.ErrDeviationRequestOutsideTransaction
	}
	if limit < 1 {
		return nil, fmt.Errorf("deviation request: a sweep reads a batch of at least one (got %d)", limit)
	}
	projects, visible, err := r.scopedProjects(ctx, uuid.Nil)
	if err != nil || !visible {
		return nil, err
	}
	if projects != nil {
		q = q.Where(deviationrequest.ProjectID.In(uuidsToRaw(projects)...))
	}
	ex, err := r.conn.conn.Executor(ctx)
	if err != nil {
		return nil, err
	}
	q = q.Order(deviationrequest.ExpiresAt.Asc(), deviationrequest.ID.Asc())
	if !after.AtStart() {
		q = q.After(deviationrequest.Row{ExpiresAt: after.ExpiresAt, ID: after.ID})
	}
	rows, err := q.
		Limit(int64(min(limit, maxSweepBatch))).
		ForUpdateSkipLocked().
		All(ctx, ex, nil)
	if err != nil {
		return nil, fmt.Errorf("could not read the requests the clock has closed: %w", err)
	}
	out := make([]entities.DeviationRequest, 0, len(rows))
	for _, row := range rows {
		request, err := queuedRequestFrom(queueRowOf(row))
		if err != nil {
			return nil, err
		}
		out = append(out, request)
	}
	return out, nil
}
