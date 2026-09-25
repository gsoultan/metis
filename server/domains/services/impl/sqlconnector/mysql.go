package sqlconnector

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
)

// unknownSystemVariable is MySQL's error for a SET of a variable it does not
// have — which is how MariaDB answers max_execution_time.
const unknownSystemVariable = 1193

// mysqlDialect reads MySQL and MariaDB.
type mysqlDialect struct{}

func (mysqlDialect) label() string { return "MySQL" }

// open forces three settings a connection string could otherwise turn on:
//
//   - multiStatements, which would run "SELECT 1; DELETE ..." as two;
//   - allowAllFiles, which lets the server ask for any file on this machine
//     through LOAD DATA LOCAL INFILE — the server here is somebody else's;
//   - interpolateParams, which splices values into the text client-side
//     rather than sending them as parameters.
//
// parseTime is forced on so a DATETIME arrives as a time, not as bytes.
func (mysqlDialect) open(dsn string, hosts hostPolicy) (*sql.DB, error) {
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, errUnreadableConnection
	}
	if err := hosts.permit(hostOf(config.Addr)); err != nil {
		return nil, err
	}
	hardenMySQL(config)
	connector, err := mysql.NewConnector(config)
	if err != nil {
		return nil, errUnreadableConnection
	}
	return sql.OpenDB(connector), nil
}

// hardenMySQL forces what open's note describes, whatever the connection
// string asked for.
func hardenMySQL(config *mysql.Config) {
	config.MultiStatements = false
	config.AllowAllFiles = false
	config.InterpolateParams = false
	config.ParseTime = true
}

// lexModes: backslash escapes are on unless NO_BACKSLASH_ESCAPES is set.
func (mysqlDialect) lexModes() []lexMode {
	return []lexMode{{backslashEscapes: true}, {backslashEscapes: false}}
}

func (mysqlDialect) placeholders() placeholderStyle {
	return placeholderStyle{
		format: func(int) string { return "?" },
		reuse:  false,
		arg:    func(_ int, value any) any { return value },
	}
}

// begin uses START TRANSACTION READ ONLY, which MySQL enforces: a write inside
// it fails. The time limit is max_execution_time, or max_statement_time on
// MariaDB, which does not know the first.
func (mysqlDialect) begin(ctx context.Context, db *sql.DB, limit time.Duration) (*sql.Tx, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, fmt.Sprintf(mysqlStatementTimeout, limit.Milliseconds()))
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == unknownSystemVariable {
		_, err = tx.ExecContext(ctx, fmt.Sprintf(mariadbStatementTimeout, limit.Seconds()))
	}
	if err != nil {
		rollback(tx)
		return nil, err
	}
	return tx, nil
}
