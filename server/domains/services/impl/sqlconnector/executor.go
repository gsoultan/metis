package sqlconnector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/rs/zerolog/log"

	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// Key is the database lookup's name in the connector catalogue.
const Key = "sql-query"

// clientGrace is how much longer than the server's own limit this side waits,
// so the server's timeout — which stops the query — is the one that fires, and
// this deadline only catches a server that never answers at all.
const clientGrace = time.Second

var (
	errNoResultVariable  = errors.New("the step does not say which variable to store the lookup's answer in")
	errBadResultVariable = errors.New("the variable a lookup's answer is stored in must start with a letter " +
		"and use only letters, digits and underscores")

	// resultVariablePattern keeps the answer's name one a FEEL expression can
	// read — customer.row.tier — and off the leading underscore the engine
	// uses for its own bookkeeping.
	resultVariablePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`)
)

// Executor is the database lookup connector.
type Executor struct {
	pools *pools
}

// The executor takes a step's request, and is asserted to so that losing it
// is a build failure rather than a lookup that quietly runs without its query.
var _ servicecontracts.RequestExecutor = (*Executor)(nil)

// New builds the executor from the environment's pool and host settings.
func New() *Executor {
	return &Executor{pools: newPools(resolvePoolSettings(), resolveHostPolicy())}
}

// Execute tests a connection: it is what the Connectors page's "Test" runs,
// with the settings an administrator is filling in. It opens the pool — which
// holds the host to the operator's list, forces the settings a lookup depends
// on, and refuses a login that can see Metis's own tables — pings it, and runs
// nothing of anybody's.
//
// It arrives only through POST /connectors/execute, which takes its caller's
// configuration and is therefore an administrator's alone (endpoints.go; the
// wiring test drives it through the real HTTP chain). The engine never calls
// it: a lookup step comes in through ExecuteRequest.
func (e *Executor) Execute(ctx context.Context, config map[string]any, _ map[string]any) (map[string]any, error) {
	conn, err := connectionFrom(config)
	if err != nil {
		return nil, err
	}
	db, err := e.pools.get(ctx, conn)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, conn.timeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, describeFailure(conn, err)
	}
	return map[string]any{"status": "connected", "database": conn.dialect.label()}, nil
}

// ExecuteRequest runs one step's lookup and returns its answer under the
// step's result variable.
func (e *Executor) ExecuteRequest(ctx context.Context, config map[string]any, req servicecontracts.ConnectorRequest) (map[string]any, error) {
	if err := validateResultVariable(req.ResultVariable); err != nil {
		return nil, err
	}
	conn, err := connectionFrom(config)
	if err != nil {
		return nil, err
	}
	if err := validateStatement(req.Statement, conn.dialect); err != nil {
		return nil, err
	}
	query, args, err := bind(req.Statement, req.Params, conn.dialect)
	if err != nil {
		return nil, err
	}
	db, err := e.pools.get(ctx, conn)
	if err != nil {
		return nil, err
	}
	read, err := lookup(ctx, db, conn, query, args)
	if err != nil {
		return nil, describeFailure(conn, err)
	}
	return map[string]any{req.ResultVariable: read.envelope()}, nil
}

func validateResultVariable(name string) error {
	if name == "" {
		return errNoResultVariable
	}
	if !resultVariablePattern.MatchString(name) {
		return errBadResultVariable
	}
	return nil
}

// lookup runs the query in a transaction that is always rolled back.
//
// When a limit stops the read, the query is cancelled rather than drained:
// closing a result early makes some drivers read every remaining row, and the
// point of the limit was not to.
func lookup(ctx context.Context, db *sql.DB, conn connection, query string, args []any) (rowsRead, error) {
	ctx, cancel := context.WithTimeout(ctx, conn.timeout+clientGrace)
	defer cancel()
	read, err := lookupWithin(ctx, db, conn, query, args)
	// SQL Server has no statement timeout of its own; its driver reports the
	// lookup's deadline however it likes. The deadline is ours, so this side is
	// what says a lookup ran out of time.
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return rowsRead{}, fmt.Errorf("%w: %w", context.DeadlineExceeded, err)
	}
	return read, err
}

func lookupWithin(ctx context.Context, db *sql.DB, conn connection, query string, args []any) (rowsRead, error) {
	tx, err := conn.dialect.begin(ctx, db, conn.timeout)
	if err != nil {
		return rowsRead{}, err
	}
	defer rollback(tx)

	queryCtx, stop := context.WithCancel(ctx)
	defer stop()
	rows, err := tx.QueryContext(queryCtx, query, args...)
	if err != nil {
		return rowsRead{}, err
	}
	defer closeRows(rows)
	read, err := readRows(rows, conn.limits)
	if err != nil {
		return rowsRead{}, err
	}
	if read.truncated {
		// Before the deferred close, so it returns at once instead of reading
		// what is left of a result nobody will use.
		stop()
	}
	return read, nil
}

// closeRows closes a lookup's result. An error here is not the lookup's: the
// rows were read, or the read already failed and said so, or the query was
// cancelled on purpose at a limit.
func closeRows(rows *sql.Rows) {
	if err := rows.Close(); err != nil {
		log.Debug().Err(err).Msg("A lookup's result did not close cleanly")
	}
}
