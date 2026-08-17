package gorms

import (
	"context"
	"sync"

	"github.com/gsoultan/gobpm/server/repositories/contracts"

	"gorm.io/gorm"
)

var (
	dbOverrideMu sync.RWMutex
	dbOverride   *gorm.DB
)

// SetDBOverride replaces the active database connection used by all repositories.
// This is called after first-time setup to switch from the temporary SQLite database
// to the user-configured target database without requiring an application restart.
func SetDBOverride(db *gorm.DB) {
	dbOverrideMu.Lock()
	defer dbOverrideMu.Unlock()
	dbOverride = db
}

// ResolveDB returns the override database if set, otherwise the original database.
// ResolveDB applies the hot-swap override. Repository methods must not call it
// directly — use GetTx, which both applies the override and joins any active
// unit-of-work transaction. Five repositories called this directly and their
// writes silently escaped every transaction they ran inside; with SQLite's
// single connection the same call graph deadlocked against itself, which is
// how it was finally noticed.
func ResolveDB(db *gorm.DB) *gorm.DB {
	dbOverrideMu.RLock()
	defer dbOverrideMu.RUnlock()
	if dbOverride != nil {
		return dbOverride
	}
	return db
}

// Config is the GORM configuration every connection to this schema must use.
//
// It exists so there is one answer rather than one per gorm.Open call site.
// Test harnesses must use it too: a setting the tests do not share is a setting
// the tests do not check.
func Config() *gorm.Config {
	return &gorm.Config{
		// TranslateError turns each driver's own way of saying "that row already
		// exists" into gorm.ErrDuplicatedKey. Without it, recognising a unique
		// constraint means matching error text per dialect — four spellings to
		// keep in step, and the wrong one is discovered in production. The
		// definition version allocator depends on it.
		TranslateError: true,
	}
}

type contextKey string

const (
	txKey contextKey = "gorm_tx"
)

type gormUnitOfWork struct {
	db *gorm.DB
}

// NewUnitOfWork creates a new GORM-based UnitOfWork.
func NewUnitOfWork(db *gorm.DB) contracts.UnitOfWork {
	return &gormUnitOfWork{db: db}
}

func (u *gormUnitOfWork) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey).(*gorm.DB); ok {
		// Already in a transaction; reuse it.
		return fn(ctx)
	}
	return ResolveDB(u.db).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, txKey, tx)
		return fn(txCtx)
	})
}

// Attempt runs fn so that its failure can be recovered from.
//
// Do reuses an enclosing transaction, which is right for work that must succeed
// or roll back with everything around it. It is wrong for work the caller
// intends to retry: on PostgreSQL a failed statement poisons its transaction, so
// the retry runs inside a connection that will refuse everything until rollback,
// and the loop returns the same error until it gives up.
//
// Attempt takes a savepoint instead, so a failed try rolls back to the point
// just before it and leaves the enclosing transaction usable. Outside a
// transaction it behaves exactly like Do.
func (u *gormUnitOfWork) Attempt(ctx context.Context, fn func(ctx context.Context) error) error {
	if tx, ok := ctx.Value(txKey).(*gorm.DB); ok {
		return tx.Transaction(func(saved *gorm.DB) error {
			return fn(context.WithValue(ctx, txKey, saved))
		})
	}
	return u.Do(ctx, fn)
}

// GetTx retrieves the transaction from the context, if present.
func GetTx(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(txKey).(*gorm.DB); ok {
		return tx
	}
	return ResolveDB(db).WithContext(ctx)
}
