package contracts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

// ErrDeviationRequestOutsideTransaction is what a write, a locking read or a
// sweep's read answers when no transaction is open. A request written apart
// from the change that asks for it can outlive a change that rolled back, and
// a row lock taken outside a transaction is let go before the next statement.
var ErrDeviationRequestOutsideTransaction = errors.New("a request for a second administrator is written, locked and decided inside a transaction, and none is open")

// ErrDeviationRequestDecided is what Transition answers when the request is
// no longer at the status the caller read it at: somebody else decided it
// first, and their decision stands.
var ErrDeviationRequestDecided = errors.New("this request was decided by somebody else first")

// ErrDeviationRequestAlreadyWaiting is what Create answers when the project
// already has a live request — waiting, or approved and running — with the
// same fingerprint. The database decides it (the unique index on the live
// key), so of two asks made at the same instant one is written and the other
// gets this.
//
// It is a state, not a mistake, and it has no class a client is answered
// with: the service that asked says what it means. The transaction it
// happened in is over — PostgreSQL refuses every later statement until it is
// rolled back — so return the error from the unit of work, and read the live
// request, if it is wanted, in a new one.
var ErrDeviationRequestAlreadyWaiting = errors.New("an identical request is already waiting for approval")

// DeviationRequestChange is one move of a request from one status to another,
// with what is known at that moment.
//
// Status is where the request goes and is always given. Every other field is
// left as stored when it is zero; a nil Outcome keeps the stored one, and an
// empty one that is not nil replaces it with nothing.
//
// A decision a person makes has to say who and when. A move that would leave
// a request approved, applied or rejected without a DecidedBy, a DecidedByID
// and a DecidedAt — given by the change, or already stored — is refused: a
// request approved by no account reads as one a second person approved, and
// one approved at no time as a run that never reported. So a waiting request
// is approved, applied or rejected only by a change that gives all three; a
// run's report on an approved request (applied) gives none and keeps the
// approval's. An expiry, a stale request and an interruption name nobody: the
// clock, the instance or a failed run made them.
//
// Who approved is written once. A change that moves a request out of approved
// or interrupted is a report — the run's, or the sweep's — and says what
// happened: Status and Outcome. One that gives a DecidedBy, a DecidedByID, a
// DecidedAt or a DecisionReason is refused, whatever their values.
type DeviationRequestChange struct {
	Status         entities.DeviationRequestStatus
	DecidedBy      string
	DecidedByID    uuid.UUID
	DecisionReason string
	DecidedAt      time.Time
	Outcome        map[string]any
}

// DeviationRequestReader reads requests without locking them. Both reads are
// scoped to the caller's organization: another organization's request is not
// found.
type DeviationRequestReader interface {
	// Get answers one request, whole and as stored. Its Status is the stored
	// one; what it reads as at a given moment is EffectiveStatus.
	Get(ctx context.Context, id uuid.UUID) (entities.DeviationRequest, error)

	// GetReadable answers one request as stored, with each of its sealed
	// documents that opens. A document that does not open is left nil —
	// Command, Plan or ApprovedInstances, which a whole request never has
	// nil — where Get would fail. It is for whoever has to see a request, or
	// find out whether it still waits, without needing all of what it asked:
	// a request whose plan no longer opens is still one somebody has to be
	// able to read, reject and close. Whoever acts on what a request asked
	// for reads it whole.
	GetReadable(ctx context.Context, id uuid.UUID) (entities.DeviationRequest, error)

	// List answers one page of the caller's requests, newest first, and how
	// many there are in all. A status in the query is the status a request
	// reads as at now (EffectiveStatus): a waiting request past its deadline
	// is listed as expired and not as pending, whatever the table says. Each
	// row's Status is still the stored one.
	//
	// A listed request has no Command, no Plan and no ApprovedInstances —
	// they are nil, and so is what Because reads from the plan. Those three
	// are the heavy part of a request, one of them without a bound, and a
	// page does not read them: Get does, one request at a time.
	List(ctx context.Context, query entities.DeviationRequestQuery, now time.Time) ([]entities.DeviationRequest, int64, error)
}

// DeviationRequestWriter writes requests and moves them. Create, both locking
// reads and Transition are refused outside a transaction
// (ErrDeviationRequestOutsideTransaction).
type DeviationRequestWriter interface {
	// Create writes a request that waits for approval. A second live request
	// with the same fingerprint in the project is
	// ErrDeviationRequestAlreadyWaiting. A request the caller got wrong — no
	// project, requester, fingerprint or deadline, a status other than
	// pending_approval, a waive of no instance, a migration between no
	// versions — is a plain error. An instance or a version that is not in
	// the request's project is not found.
	Create(ctx context.Context, request entities.DeviationRequest) (entities.DeviationRequest, error)

	// GetForUpdate answers one request, whole, and holds its row until the
	// transaction ends. A request's row is locked before the instance's,
	// everywhere: request, then instance, then task rows.
	GetForUpdate(ctx context.Context, id uuid.UUID) (entities.DeviationRequest, error)

	// GetForUpdateWithoutDocuments holds one request's row as GetForUpdate
	// does — the same lock, the same scope, the same place in the order — and
	// answers it as the queue reads it: no Command, no Plan, no
	// ApprovedInstances. It is the read of whoever ends a request without
	// carrying it out — a rejection, a withdrawal, the closing of an overdue
	// one — none of which needs what the request asked, so none of which
	// fails because a sealed document no longer opens. An approval reads
	// with GetForUpdate: what it is about to do is in those documents.
	GetForUpdateWithoutDocuments(ctx context.Context, id uuid.UUID) (entities.DeviationRequest, error)

	// FindLive answers the project's live request with this fingerprint, if
	// there is one, whole and as stored. It locks nothing.
	//
	// Live is what the table says, not what the clock says. The request found
	// may be approved rather than waiting, and it may be past its deadline —
	// or past its run window — with no sweep having closed it yet. Read it
	// through EffectiveStatus(now) before treating it as waiting.
	FindLive(ctx context.Context, projectID uuid.UUID, fingerprint string) (entities.DeviationRequest, bool, error)

	// Transition moves a request from the status the caller read it at to
	// change.Status, and answers the row as it then is: whole — or, when its
	// sealed documents no longer open, without them (Command, Plan and
	// ApprovedInstances nil, which a whole request never has); the move is
	// written either way, so such a request can still be closed. It holds the row
	// while it does (request before instance, as GetForUpdate), so of two
	// moves made at once one is written and the other is
	// ErrDeviationRequestDecided — as is any move from a status the request
	// is not at.
	//
	// Only the moves the product makes are written, and they differ by kind.
	// A waive that waits is applied, rejected, expired or made stale — never
	// approved without being applied. A migration that waits is approved,
	// rejected, expired or made stale — never applied without having been
	// approved; approved, it is applied or interrupted; interrupted, it is
	// reported on by its run (applied, or interrupted again). Any other move
	// — one that another kind makes included — a status outside the
	// closed set, a person's decision that does not say who and when, or a
	// report that names a decision (see DeviationRequestChange) is a plain
	// error, and nothing is written.
	Transition(ctx context.Context, id uuid.UUID, from entities.DeviationRequestStatus, change DeviationRequestChange) (entities.DeviationRequest, error)
}

// SweepCursor is how far a sweep has read: the deadline and the id of the last
// request it was answered. The sweep's reads are ordered by deadline and then
// by id, and answer only what comes strictly after the cursor. The zero
// cursor is the start.
//
// It is what lets a sweep get past a request it could not close. Read from
// the start each time, such a request would be answered again and again —
// requests that cannot be closed only get older, and the oldest is read
// first — and nothing behind it would ever be reached.
type SweepCursor struct {
	ExpiresAt time.Time
	ID        uuid.UUID
}

// SweepCursorAt is the cursor that stands at a request: a read after it
// answers what follows that request.
func SweepCursorAt(request entities.DeviationRequest) SweepCursor {
	return SweepCursor{ExpiresAt: request.ExpiresAt, ID: request.ID}
}

// AtStart reports whether the cursor is the zero one, before every request.
func (c SweepCursor) AtStart() bool {
	return c.ID == uuid.Nil && c.ExpiresAt.IsZero()
}

// DeviationRequestSweeper reads what the clock has closed and nobody has
// written down yet. All of it is refused outside a transaction
// (ErrDeviationRequestOutsideTransaction): the reads hold the rows they
// answer until it ends, and pass by a row somebody else holds — that one is
// being decided, or reported on, right now. So a read can answer fewer than
// limit while more remain, and what it passed by is simply not answered: the
// cursor moves beyond it, and the next pass meets it. A row is moved with
// Transition, which writes only if the status is still the one read.
//
// The reads answer requests as List does — no Command, no Plan, no
// ApprovedInstances — because a sweep closes requests and has no use for what
// they asked, and one request whose sealed plan no longer opens must not fail
// the read it is in. Whoever needs a swept request's documents reads it with
// GetForUpdate. Run as system work they see every organization's requests, a
// deleted project's among them; run for one organization, only its own.
type DeviationRequestSweeper interface {
	// ListOverdue answers up to limit requests that still wait at or after
	// their deadline, the longest overdue first, strictly after the cursor.
	ListOverdue(ctx context.Context, now time.Time, after SweepCursor, limit int) ([]entities.DeviationRequest, error)

	// ListUnreported answers up to limit approved requests whose run window
	// has closed at now, as RunWindowClosed says — the deadline has come, the
	// report window has passed since the approval, or the approval has no
	// time at all — in the same order, strictly after the cursor.
	ListUnreported(ctx context.Context, now time.Time, after SweepCursor, limit int) ([]entities.DeviationRequest, error)

	// BoundLockWait makes the transaction it is called in give up on a row
	// lock it has waited that long for (SET LOCAL lock_timeout): the
	// statement that was waiting fails, and with it the transaction. It is how a sweep
	// closes one request without standing behind whoever holds a row that
	// request needs, with the rest of its pass behind it. A wait that is not
	// longer than nothing is a plain error.
	BoundLockWait(ctx context.Context, wait time.Duration) error
}

// DeviationRequestRepository stores the requests that wait for a second
// administrator.
type DeviationRequestRepository interface {
	DeviationRequestReader
	DeviationRequestWriter
	DeviationRequestSweeper
}
