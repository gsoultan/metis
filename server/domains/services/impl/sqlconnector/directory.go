package sqlconnector

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// PreparePostgres readies a connection string a project's administrator wrote
// for a read of another database, outside a lookup: a People directory sync.
//
// The same rules as a lookup's connection. The hosts the string can reach,
// fallbacks included, must be ones the operator allows under
// METIS_SQL_LOOKUP_ALLOWED_HOSTS, when that is set; the extended protocol is
// forced, so one query is one statement; and every transaction is read-only by
// default. applicationName names the work in pg_stat_activity, when the string
// does not.
func PreparePostgres(dsn, applicationName string) (*pgx.ConnConfig, error) {
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, errUnreadableConnection
	}
	reached := []string{config.Host}
	for _, fallback := range config.Fallbacks {
		reached = append(reached, fallback.Host)
	}
	if err := resolveHostPolicy().permit(reached...); err != nil {
		return nil, err
	}
	if config.RuntimeParams["application_name"] == "" && applicationName != "" {
		config.RuntimeParams["application_name"] = applicationName
	}
	hardenPostgres(config)
	return config, nil
}

// RefuseOwnPostgres refuses a connection whose login can see Metis's own
// tables, the check a lookup's pool makes before its first query. See
// refuseOwnDatabase for why it asks the database rather than comparing
// addresses.
func RefuseOwnPostgres(ctx context.Context, conn *pgx.Conn) error {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	var visible int
	if err := conn.QueryRow(ctx, ownTablesProbe).Scan(&visible); err != nil {
		return fmt.Errorf("could not confirm the database is not Metis's own, so it was not used: %w", err)
	}
	if visible > 0 {
		return errOwnDatabase
	}
	return nil
}

// StatementTimeout is the statement that ends a PostgreSQL query running
// longer than milliseconds, inside the current transaction only.
func StatementTimeout(milliseconds int64) string {
	return fmt.Sprintf(postgresStatementTimeout, milliseconds)
}
