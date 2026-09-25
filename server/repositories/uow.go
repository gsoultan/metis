package repositories

import (
	"context"

	"github.com/gsoultan/metis/server/repositories/contracts"
	stormdb "github.com/gsoultan/metis/server/repositories/db"
)

// stormUnitOfWork runs work inside one database transaction.
//
// It briefly had to open a transaction on both persistence layers, because a
// single logical operation could touch repositories on either side and the two
// held separate connections — so a rollback undid half of it, which is how a
// compensation that failed was recorded as done. Every repository is on storm
// now, so there is one transaction again and the window is closed.
type stormUnitOfWork struct{ conn *stormdb.Conn }

func newUnitOfWork(conn *stormdb.Conn) contracts.UnitOfWork {
	return &stormUnitOfWork{conn: conn}
}

// Do runs fn inside a transaction, reusing an enclosing one rather than nesting.
//
// A repository that opened its own would have its writes commit or roll back
// independently of the work around them, which is the bug that made five GORM
// repositories silently escape every transaction they ran inside.
func (u *stormUnitOfWork) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return u.conn.Transact(ctx, fn)
}

// Attempt runs fn so that its failure can be recovered from.
//
// A savepoint rather than the whole transaction: on PostgreSQL a failed
// statement poisons its transaction, so a retry without one runs against a
// connection refusing everything until rollback. Losing the race for a version
// number is the normal case under concurrency, so it has to be recoverable.
func (u *stormUnitOfWork) Attempt(ctx context.Context, fn func(ctx context.Context) error) error {
	return u.conn.Attempt(ctx, fn)
}

// AfterCommit runs fn once the transaction around ctx commits.
func (u *stormUnitOfWork) AfterCommit(ctx context.Context, fn func()) {
	stormdb.AfterCommit(ctx, fn)
}
