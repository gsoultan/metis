package sqlconnector

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// postgresDialect reads PostgreSQL.
type postgresDialect struct{}

func (postgresDialect) label() string { return "PostgreSQL" }

// open forces the extended protocol, which runs exactly one statement per
// query. The simple protocol — which a connection string can ask for with
// default_query_exec_mode=simple_protocol — would run "SELECT 1; DELETE ..." as
// two. describe_exec rather than the cached default, so a pooler in transaction
// mode, which is why somebody asks for the simple protocol, still works.
//
// Every transaction on these connections is read-only by default as well as
// being begun read-only, so the protection does not rest on one statement.
func (postgresDialect) open(dsn string, hosts hostPolicy) (*sql.DB, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, errUnreadableConnection
	}
	// A connection string can name fallback hosts, tried when the first is
	// down; each is somewhere this connection may go.
	reached := []string{config.Host}
	for _, fallback := range config.Fallbacks {
		reached = append(reached, fallback.Host)
	}
	if err := hosts.permit(reached...); err != nil {
		return nil, err
	}
	hardenPostgres(config)
	return stdlib.OpenDB(*config), nil
}

// hardenPostgres forces what open's note describes, whatever the connection
// string asked for.
func hardenPostgres(config *pgx.ConnConfig) {
	if config.DefaultQueryExecMode == pgx.QueryExecModeSimpleProtocol {
		config.DefaultQueryExecMode = pgx.QueryExecModeDescribeExec
	}
	config.RuntimeParams["default_transaction_read_only"] = "on"
	if config.RuntimeParams["application_name"] == "" {
		config.RuntimeParams["application_name"] = applicationName
	}
}

// lexModes: standard_conforming_strings is on by default, so a backslash is
// ordinary in a string; it was not always, and E” strings still escape.
func (postgresDialect) lexModes() []lexMode {
	return []lexMode{{backslashEscapes: false}, {backslashEscapes: true}}
}

func (postgresDialect) placeholders() placeholderStyle {
	return placeholderStyle{
		format: func(ordinal int) string { return fmt.Sprintf("$%d", ordinal) },
		reuse:  true,
		arg:    func(_ int, value any) any { return value },
	}
}

func (postgresDialect) begin(ctx context.Context, db *sql.DB, limit time.Duration) (*sql.Tx, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(postgresStatementTimeout, limit.Milliseconds())); err != nil {
		rollback(tx)
		return nil, err
	}
	return tx, nil
}
