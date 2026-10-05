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

// DeviationRequestWriter writes requests and moves them. Create, GetForUpdate
// and Transition are refused outside a transaction
// (ErrDeviationRequestOutsideTransaction).
type DeviationRequestWriter interface {
	// Create writes a request that waits for approval. A second live request
	// with the same fingerprint in the project is
	// ErrDeviationRequestAlreadyWaiting. A request the caller got wrong — no
	// project, requester, fingerprint or deadline, a status other than
	// pending_approval, a waive of no instance, a migration between no
	// versions — is a plain error.
	Create(ctx context.Context, request entities.DeviationRequest) (entities.DeviationRequest, error)

	// GetForUpdate answers one request, whole, and holds its row until the
	// transaction ends. A request's row is locked before the instance's,
	// everywhere: request, then instance, then task rows.
	GetForUpdate(ctx context.Context, id uuid.UUID) (entities.DeviationRequest, error)

	// FindLive answers the project's live request with this fingerprint, if
	// there is one, whole and as stored. It locks nothing.
	//
	// Live is what the table says, not what the clock says. The request found
	// may be approved rather than waiting, and it may be past its deadline —
	// or past its run window — with no sweep having closed it yet. Read it
	// through EffectiveStatus(now) before treating it as waiting.
	FindLive(ctx context.Context, projectID uuid.UUID, fingerprint string) (entities.DeviationRequest, bool, error)

	// Transition moves a request from the status the caller read it at to
	// change.Status, and answers the row as it then is. It holds the row
	// while it does (request before instance, as GetForUpdate), so of two
	// moves made at once one is written and the other is
	// ErrDeviationRequestDecided — as is any move from a status the request
	// is not at.
	//
	// Only the moves the product makes are written: a waiting request is
	// approved, applied, rejected, expired or made stale; an approved one is
	// applied or interrupted; an interrupted one is reported on by its run
	// (applied, or interrupted again). Any other move, a status outside the
	// closed set, or a person's decision that does not say who and when
	// (see DeviationRequestChange) is a plain error, and nothing is written.
	Transition(ctx context.Context, id uuid.UUID, from entities.DeviationRequestStatus, change DeviationRequestChange) (entities.DeviationRequest, error)
}

// DeviationRequestSweeper reads what the clock has closed and nobody has
// written down yet. Both reads are refused outside a transaction
// (ErrDeviationRequestOutsideTransaction): they hold the rows they answer
// until it ends, and pass by a row somebody else holds — that one is being
// decided, or reported on, right now. So a batch can be shorter than limit
// while more remain; and a row is moved with Transition, which writes only if
// the status is still the one read.
//
// They answer requests whole. Run as system work they see every
// organization's, a deleted project's among them; run for one organization,
// only its own.
type DeviationRequestSweeper interface {
	// ListOverdue answers up to limit requests that still wait at or after
	// their deadline, the longest overdue first.
	ListOverdue(ctx context.Context, now time.Time, limit int) ([]entities.DeviationRequest, error)

	// ListUnreported answers up to limit approved requests whose run window
	// has closed at now, as RunWindowClosed says: the deadline has come, the
	// report window has passed since the approval, or the approval has no
	// time at all.
	ListUnreported(ctx context.Context, now time.Time, limit int) ([]entities.DeviationRequest, error)
}

// DeviationRequestRepository stores the requests that wait for a second
// administrator.
type DeviationRequestRepository interface {
	DeviationRequestReader
	DeviationRequestWriter
	DeviationRequestSweeper
}
