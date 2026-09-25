package contracts

import "context"

// UnitOfWork defines the interface for managing database transactions.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error

	// Attempt runs fn as a unit the caller may retry after a failure. Inside an
	// enclosing transaction it takes a savepoint, so a failed try does not leave
	// that transaction unusable; outside one it is Do.
	Attempt(ctx context.Context, fn func(ctx context.Context) error) error

	// AfterCommit runs fn once the transaction ctx belongs to has committed,
	// and never if it rolls back; outside a transaction it runs fn now. It is
	// for the work that must not happen inside one — a network call holds the
	// transaction's connection and locks for as long as somebody else's server
	// takes to answer, and happens even when the transaction is undone.
	AfterCommit(ctx context.Context, fn func())
}
