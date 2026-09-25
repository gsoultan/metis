package sqlconnector

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	mssql "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
)

// sqlServerParamPrefix names the lookup's parameters so one written by hand in
// the query — @p1 is the driver's own convention — cannot take one of them.
const sqlServerParamPrefix = "metis_p"

// sqlServerDialect reads SQL Server.
type sqlServerDialect struct{}

func (sqlServerDialect) label() string { return "SQL Server" }

func (sqlServerDialect) open(dsn string, hosts hostPolicy) (*sql.DB, error) {
	config, err := msdsn.Parse(dsn)
	if err != nil {
		return nil, errUnreadableConnection
	}
	if err := hosts.permit(config.Host); err != nil {
		return nil, err
	}
	return sql.OpenDB(mssql.NewConnectorConfig(config)), nil
}

// lexModes: [name] is a quoted identifier. Backslashes are ordinary in T-SQL
// strings; they are read both ways anyway, since that costs nothing and a
// reading that is wrong costs a great deal.
func (sqlServerDialect) lexModes() []lexMode {
	return []lexMode{
		{backslashEscapes: false, bracketIdentifiers: true},
		{backslashEscapes: true, bracketIdentifiers: true},
	}
}

func (sqlServerDialect) placeholders() placeholderStyle {
	return placeholderStyle{
		format: func(ordinal int) string { return fmt.Sprintf("@%s%d", sqlServerParamPrefix, ordinal) },
		reuse:  true,
		arg: func(ordinal int, value any) any {
			return sql.Named(fmt.Sprintf("%s%d", sqlServerParamPrefix, ordinal), value)
		},
	}
}

// begin has no read-only mode to ask for: SQL Server has none. The transaction
// is there to be rolled back, so a change the query should never have been
// able to make is undone — and a lock the lookup cannot get is waited for no
// longer than its time limit.
func (sqlServerDialect) begin(ctx context.Context, db *sql.DB, limit time.Duration) (*sql.Tx, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(sqlServerLockTimeout, limit.Milliseconds())); err != nil {
		rollback(tx)
		return nil, err
	}
	return tx, nil
}
