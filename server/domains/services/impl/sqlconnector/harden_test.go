package sqlconnector

import (
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
)

// A connection string is an administrator's, and could ask for the settings
// that would let one query run two statements, or let the database read files
// from this machine. These run with no database: they are about what the
// connector forces, whatever it was given.
func TestPostgresIsHeldToTheExtendedProtocolAndReadOnly(t *testing.T) {
	config, err := pgx.ParseConfig("postgres://svc:pw@db.internal/crm?default_query_exec_mode=simple_protocol&application_name=theirs")
	if err != nil {
		t.Fatal(err)
	}
	hardenPostgres(config)
	if config.DefaultQueryExecMode == pgx.QueryExecModeSimpleProtocol {
		t.Error("the simple protocol, which runs every statement in a query, was kept")
	}
	if config.RuntimeParams["default_transaction_read_only"] != "on" {
		t.Error("sessions are not read-only by default")
	}
	if config.RuntimeParams["application_name"] != "theirs" {
		t.Error("an application name the administrator chose was replaced")
	}

	config, _ = pgx.ParseConfig("postgres://svc:pw@db.internal/crm")
	hardenPostgres(config)
	if config.RuntimeParams["application_name"] != applicationName {
		t.Errorf("application_name = %q", config.RuntimeParams["application_name"])
	}
}

func TestMySQLIsHeldToOneStatementAndNoLocalFiles(t *testing.T) {
	config, err := mysql.ParseDSN("svc:pw@tcp(db.internal:3306)/crm?multiStatements=true&allowAllFiles=true&interpolateParams=true")
	if err != nil {
		t.Fatal(err)
	}
	hardenMySQL(config)
	if config.MultiStatements || config.AllowAllFiles || config.InterpolateParams || !config.ParseTime {
		t.Fatalf("multiStatements=%v allowAllFiles=%v interpolateParams=%v parseTime=%v",
			config.MultiStatements, config.AllowAllFiles, config.InterpolateParams, config.ParseTime)
	}
}
