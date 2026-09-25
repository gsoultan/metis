package sqlconnector

// The SQL this connector issues itself. The lookup's own query is the step's;
// these are the settings around it. Every value formatted into one is an
// integer this code computed, never text from a step or a connection.
const (
	// postgresStatementTimeout ends a query PostgreSQL has been running too
	// long — on the server, so the query stops rather than being abandoned.
	postgresStatementTimeout = "SET LOCAL statement_timeout = %d"

	// mysqlStatementTimeout is MySQL's limit on a SELECT, in milliseconds.
	mysqlStatementTimeout = "SET SESSION max_execution_time = %d"
	// mariadbStatementTimeout is MariaDB's, which has another name and counts
	// in seconds.
	mariadbStatementTimeout = "SET SESSION max_statement_time = %.3f"

	// sqlServerLockTimeout bounds how long a lookup waits for a lock. SQL
	// Server has no statement timeout of its own; the driver cancels a query
	// whose deadline passes.
	sqlServerLockTimeout = "SET LOCK_TIMEOUT %d"

	// ownTablesProbe counts the tables of Metis's own schema the lookup's login
	// can see. Written in capitals because SQL Server's INFORMATION_SCHEMA is
	// case-sensitive under a case-sensitive collation; PostgreSQL folds it and
	// MySQL ignores case here.
	ownTablesProbe = "SELECT COUNT(DISTINCT TABLE_NAME) FROM INFORMATION_SCHEMA.TABLES " +
		"WHERE TABLE_NAME IN ('process_instances', 'process_definitions', 'connector_instances')"
)

// ownTableCount is how many of the tables ownTablesProbe names there are.
const ownTableCount = 3
