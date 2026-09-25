package sqlconnector

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// Server error codes that mean something a person can act on.
const (
	postgresQueryCanceled = "57014"
	postgresReadOnlyTx    = "25006"
	mysqlQueryTimeout     = 3024
	mariadbQueryTimeout   = 1969
	mysqlReadOnlyTx       = 1792
)

// describeFailure says in plain words what went wrong with a lookup the server
// ran, and keeps the server's own error beneath it for whoever investigates.
//
// The driver's message is kept, not rewritten: it names the table or column
// that was wrong, which is what the author needs. None of the three drivers
// puts a password in one, and an incident's text is redacted again on the way
// in regardless.
func describeFailure(conn connection, err error) error {
	switch {
	case isTimeout(err):
		return fmt.Errorf("the lookup took longer than its limit of %s on %s; "+
			"narrow the query or raise the limit on the connector: %w", conn.timeout, conn.dialect.label(), err)
	case isReadOnlyViolation(err):
		return fmt.Errorf("the lookup's query tried to change data on %s, which a lookup may not: %w",
			conn.dialect.label(), err)
	}
	return fmt.Errorf("the lookup failed on %s: %w", conn.dialect.label(), err)
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == postgresQueryCanceled {
		return true
	}
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && (mysqlErr.Number == mysqlQueryTimeout || mysqlErr.Number == mariadbQueryTimeout)
}

func isReadOnlyViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == postgresReadOnlyTx {
		return true
	}
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == mysqlReadOnlyTx
}
