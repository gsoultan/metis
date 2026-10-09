package contracts

import (
	"errors"
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

// DeviationRequestRepository stores the requests that wait for a second
// administrator.
type DeviationRequestRepository interface {
	DeviationRequestReader
	DeviationRequestWriter
	DeviationRequestSweeper
}
