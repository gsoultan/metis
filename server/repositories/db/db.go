// Package db is the connection layer under the storm repositories.
//
// It answers one question — which database does this work belong to — and it is
// the only place that answers it. Repositories ask for an executor and get the
// right one: the transaction they are already inside, or their environment's
// database, or the main one.
//
// It replaces gorms.GetTx, which did the same job for GORM. The shape is kept
// deliberately: every read and write goes through a single resolver, so the
// choice is a property of the request rather than something each of the thirty
// repositories has to remember.
package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/dbpool"
	"github.com/gsoultan/storm/runtime"
	"github.com/gsoultan/storm/runtime/pgxdrv"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// ErrEnvironmentUnavailable is returned for work bound to an environment whose
// database is not open.
//
// It fails closed rather than falling back to the main database, because the
// fallback would be silent, would succeed, and would put one runtime's rows in
// another's store — which is precisely what running separate environments is
// meant to make impossible.
var ErrEnvironmentUnavailable = errors.New("this environment's database is not available")

// Conn holds the open pools: the main database, and one per environment.
type Conn struct {
	main *pgxpool.Pool

	mu   sync.RWMutex
	envs map[uuid.UUID]*pgxpool.Pool
}

// Open connects to the main database.
func Open(ctx context.Context, dsn string) (*Conn, error) {
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("could not open the main database: %w", err)
	}
	return &Conn{main: pool, envs: map[uuid.UUID]*pgxpool.Pool{}}, nil
}

// NewPool opens a pool the way every pool the storm repositories read through
// has to be opened. The main database's and each environment's come from here,
// so the two cannot drift apart.
//
// Through storm's constructor, which installs the parameter encoders the
// generated code assumes and refuses the text query modes its scanners cannot
// read — under those, a boolean decodes inverted without an error. The comment
// here always said the pool came from storm's constructor; the code called
// pgxpool directly, so neither held.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("could not read the connection string: %w", err)
	}

	// A pooled connection whose TCP has died looks alive until something is
	// sent on it, so the first request after a network blip fails on a
	// connection that was already gone. database/sql hid this by retrying once
	// on driver.ErrBadConn; pgx has no equivalent, and adding one here would
	// mean retrying statements that may already have run.
	//
	// So the pool checks instead. Idle connections are reaped and every one is
	// health-checked, which turns "the first caller after an outage gets an
	// error" into "the pool noticed before anybody asked".
	config.HealthCheckPeriod = 5 * time.Second
	config.MaxConnIdleTime = 30 * time.Second
	// Bounded lifetime as well, because a connection can also be quietly broken
	// by something between here and the server that keeps the socket open — a
	// load balancer, a firewall idle timeout — which no health check on this end
	// will notice until it is used.
	config.MaxConnLifetime = 30 * time.Minute

	// Sized by the same setting as the GORM pool. It was left to pgx's default
	// — the larger of 4 and the machine's CPU count — so the pool carrying most
	// of the engine's queries ignored METIS_DB_MAX_OPEN_CONNS, and on a large
	// node each replica could open far more connections than anyone had
	// planned for. A connection string that names pool_max_conns still wins:
	// that is somebody saying so on purpose.
	if !strings.Contains(dsn, "pool_max_conns") {
		config.MaxConns = int32(dbpool.Resolve("postgres").MaxOpenConns) //nolint:gosec // dbpool bounds it to 1,000,000
	}

	return pgxdrv.NewPoolConfig(ctx, config)
}

// EnvironmentPools returns every open environment's pool, for maintenance that
// has to reach each database — resealing after a key rotation is one: the
// sealed data of a runtime lives in that runtime's database.
func (c *Conn) EnvironmentPools() map[uuid.UUID]*pgxpool.Pool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	pools := make(map[uuid.UUID]*pgxpool.Pool, len(c.envs))
	for id, pool := range c.envs {
		pools[id] = pool
	}
	return pools
}

// Main is the pool for the main database, for the migration runner and for
// anything that legitimately works across environments.
func (c *Conn) Main() *pgxpool.Pool { return c.main }

// RegisterEnvironment records the open pool for one environment, returning any
// it displaced so the caller can close it once nothing is using it. Closing it
// here would break requests still in flight on it.
func (c *Conn) RegisterEnvironment(id uuid.UUID, pool *pgxpool.Pool) (replaced *pgxpool.Pool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	replaced = c.envs[id]
	c.envs[id] = pool
	return replaced
}

// ForgetEnvironment drops an environment's pool and hands it back to be closed.
func (c *Conn) ForgetEnvironment(id uuid.UUID) (*pgxpool.Pool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pool, ok := c.envs[id]
	delete(c.envs, id)
	return pool, ok
}

// Close releases every pool.
func (c *Conn) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, pool := range c.envs {
		pool.Close()
	}
	c.envs = map[uuid.UUID]*pgxpool.Pool{}
	c.main.Close()
}

// Executor resolves which database this work belongs to.
//
// A transaction already on the context wins: work inside a unit of work must
// join it, not open a second connection alongside it and deadlock against
// itself.
func (c *Conn) Executor(ctx context.Context) (runtime.Executor, error) {
	if tx, ok := txFrom(ctx); ok {
		return tx, nil
	}
	pool, err := c.poolFor(ctx)
	if err != nil {
		return nil, err
	}
	return pgxdrv.Pool{P: pool}, nil
}

// MainExecutor resolves an executor onto the main database, whatever port the
// request arrived on.
//
// Accounts, groups, organizations, projects and the environment registry are
// installation-wide facts. An environment holds a runtime — deployed models,
// instances, tasks — and its schema has the identity tables too, empty, because
// one migration list is easier to keep honest than two.
//
// Without this, every request on an environment's port would authenticate
// against that environment's empty users table and be refused: the token is
// valid, the account exists, and the database being asked is the wrong one.
//
// An ambient transaction wins only when it is a transaction on the main
// database. Work bound to an environment runs its unit of work on that
// environment's connection, and joining it would ask the empty copy.
func (c *Conn) MainExecutor(ctx context.Context) (runtime.Executor, error) {
	if _, bound := EnvironmentFrom(ctx); !bound {
		return c.Executor(ctx)
	}
	return pgxdrv.Pool{P: c.main}, nil
}

// Outside resolves an executor that deliberately ignores an enclosing
// transaction.
//
// For work that is a fact about something which has already happened and must
// not be undone with the caller's write: the SSE bus is the case, where joining
// the transaction would mean a rollback silently un-notifying browsers about
// work that did commit earlier in the same handler, and would hold the row
// invisible until commit — exactly when it is least useful.
//
// The environment binding still applies. Which database the work belongs to is
// not a transaction question.
func (c *Conn) Outside(ctx context.Context) (runtime.Executor, error) {
	pool, err := c.poolFor(ctx)
	if err != nil {
		return nil, err
	}
	return pgxdrv.Pool{P: pool}, nil
}

// poolFor picks the pool for this work, without regard to transactions.
func (c *Conn) poolFor(ctx context.Context) (*pgxpool.Pool, error) {
	environmentID, bound := EnvironmentFrom(ctx)
	if !bound {
		return c.main, nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	pool, open := c.envs[environmentID]
	if !open {
		return nil, fmt.Errorf("%w: %s", ErrEnvironmentUnavailable, environmentID)
	}
	return pool, nil
}

// Transact runs fn inside a database transaction.
//
// Already being in one reuses it rather than nesting: a repository that opened
// its own would have its writes commit or roll back independently of the work
// around them, which is the bug that made five GORM repositories silently
// escape every transaction they ran inside.
//
// Rollback on panic as well as on error, because a panic that leaves a
// transaction open holds its locks until the connection is reaped.
func (c *Conn) Transact(ctx context.Context, fn func(context.Context) error) (err error) {
	if _, inTx := txFrom(ctx); inTx {
		return fn(ctx)
	}

	pool, err := c.poolFor(ctx)
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("could not begin: %w", err)
	}

	// The rollback reads the named result, which every return below assigns
	// before deferred functions run — so the inner errors do not need to be
	// hoisted into it by hand.
	defer func() {
		if recovered := recover(); recovered != nil {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				// Logged rather than discarded: the panic is on its way up and
				// cannot carry this, but a transaction that would not roll back
				// is holding locks until the connection is reaped.
				log.Warn().Err(rollbackErr).Msg("Could not roll back a transaction while panicking")
			}
			panic(recovered)
		}
		if err != nil {
			if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				err = errors.Join(err, rollbackErr)
			}
		}
	}()

	hooks := &commitHooks{}
	if err := fn(withCommitHooks(withTx(ctx, pgxdrv.Tx{T: tx}), hooks)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("could not commit: %w", err)
	}
	hooks.run()
	return nil
}

// TransactMain runs fn inside a transaction on the main database, whichever
// runtime the work is bound to.
//
// Accounts and their memberships live in the main database, and are written
// while serving requests that may have arrived on an environment's port. There
// Transact would begin on the environment's database — and MainExecutor, which
// the account repository reads and writes through, deliberately does not join a
// transaction on another database, so every statement would run on its own,
// outside any transaction at all. Unbinding for the length of fn puts them
// inside one. A transaction the caller holds on a runtime's database is not
// joined either, for the same reason; one it holds on the main database is.
func (c *Conn) TransactMain(ctx context.Context, fn func(context.Context) error) error {
	if _, bound := EnvironmentFrom(ctx); !bound {
		return c.Transact(ctx, fn)
	}
	return c.Transact(withTx(Bind(ctx, uuid.Nil), nil), fn)
}

// Attempt runs fn so that its failure can be recovered from.
//
// Transact reuses an enclosing transaction, which is right for work that must
// succeed or roll back with everything around it. It is wrong for work the
// caller intends to retry: on PostgreSQL a failed statement poisons its
// transaction, so the retry runs inside a connection that refuses everything
// with "current transaction is aborted" until rollback, and the loop returns
// the same error until it gives up.
//
// Inside a transaction this takes a SAVEPOINT, so a failed attempt rolls back
// to just before itself and leaves the enclosing transaction usable. That is
// what makes the version allocator work: losing the race for a version number
// has to be recoverable, because losing it is the normal case under
// concurrency.
//
// Outside a transaction it is exactly Transact.
func (c *Conn) Attempt(ctx context.Context, fn func(context.Context) error) (err error) {
	executor, inTx := txFrom(ctx)
	if !inTx {
		return c.Transact(ctx, fn)
	}
	tx, ok := executor.(pgxdrv.Tx)
	if !ok {
		// Not a pgx transaction, so there is no savepoint to take. Running fn
		// directly is what Transact would do; the caller loses the ability to
		// retry, which is better than opening a second connection alongside the
		// transaction it is supposed to be inside.
		return fn(ctx)
	}

	saved, err := tx.T.Begin(ctx)
	if err != nil {
		return fmt.Errorf("could not take a savepoint: %w", err)
	}
	if hooks, ok := commitHooksFrom(ctx); ok {
		mark := hooks.mark()
		defer func() {
			if err != nil {
				hooks.rewind(mark)
			}
		}()
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			if rollbackErr := saved.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				log.Warn().Err(rollbackErr).Msg("Could not release a savepoint while panicking")
			}
			panic(recovered)
		}
		if err != nil {
			if rollbackErr := saved.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				err = errors.Join(err, rollbackErr)
			}
		}
	}()

	err = fn(withTx(ctx, pgxdrv.Tx{T: saved}))
	if err != nil {
		return err
	}
	if err = saved.Commit(ctx); err != nil {
		return fmt.Errorf("could not release a savepoint: %w", err)
	}
	return nil
}
