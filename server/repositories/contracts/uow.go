package contracts

import "context"

// UnitOfWork defines the interface for managing database transactions.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error

	// Attempt runs fn as a unit the caller may retry after a failure. Inside an
	// enclosing transaction it takes a savepoint, so a failed try does not leave
	// that transaction unusable; outside one it is Do.
	Attempt(ctx context.Context, fn func(ctx context.Context) error) error
}
