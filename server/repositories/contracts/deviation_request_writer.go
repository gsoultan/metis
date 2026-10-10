package contracts

import (
	"context"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/server/domains/entities"
)

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
