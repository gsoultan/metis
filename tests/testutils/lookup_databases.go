package testutils

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb" // registers the sqlserver driver

	"github.com/gsoultan/metis/internal/pkg/envvar"
)

// Servers a database lookup test may create databases and logins on. Each is
// an administrator's connection to the server, not to a database.
const (
	// MySQLDSNEnv is a go-sql-driver DSN, e.g. root:secret@tcp(127.0.0.1:3306)/
	MySQLDSNEnv = "METIS_TEST_MYSQL_DSN"
	// SQLServerDSNEnv is a go-mssqldb URL, e.g.
	// sqlserver://sa:Secret123!@127.0.0.1:1433?encrypt=disable
	SQLServerDSNEnv = "METIS_TEST_SQLSERVER_DSN"
)

// LookupDatabase is somebody else's database, as a database lookup reads it:
// its own database on the server, with a login that may read its tables and
// nothing else. That is the setup the connector's documentation asks for, so it
// is the one the tests exercise.
type LookupDatabase struct {
	// DSN connects as the reading login.
	DSN string
	// AdminDSN connects as the administrator that built it, to look behind the
	// lookup's back.
	AdminDSN string
}

// PostgresLookupDatabase builds one on the server METIS_TEST_POSTGRES_DSN
// names, running setup in it as the administrator first.
func PostgresLookupDatabase(t *testing.T, setup ...string) LookupDatabase {
	t.Helper()
	serverDSN := envvar.Get(PostgresDSNEnv)
	if serverDSN == "" {
		t.Skipf("set %s to run this against a live PostgreSQL instance", PostgresDSNEnv)
	}
	server, err := pgx.ParseConfig(serverDSN)
	if err != nil {
		t.Fatalf("read %s: %v", PostgresDSNEnv, err)
	}
	name, password := "lookup_"+randomHex(t, 4), "Rd-"+randomHex(t, 12)

	admin := openSQL(t, "pgx", serverDSN)
	mustExec(t, admin, "CREATE DATABASE "+name)
	mustExec(t, admin, fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s'", name, password))
	t.Cleanup(func() {
		// FORCE, because the lookup's pool still holds connections to it.
		dropQuietly(admin, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		dropQuietly(admin, "DROP ROLE IF EXISTS "+name)
	})

	adminDSN := postgresURL(server, server.User, server.Password, name)
	db := openSQL(t, "pgx", adminDSN)
	for _, statement := range setup {
		mustExec(t, db, statement)
	}
	mustExec(t, db, "GRANT USAGE ON SCHEMA public TO "+name)
	mustExec(t, db, "GRANT SELECT ON ALL TABLES IN SCHEMA public TO "+name)
	return LookupDatabase{DSN: postgresURL(server, name, password, name), AdminDSN: adminDSN}
}

// MySQLLookupDatabase builds one on the server METIS_TEST_MYSQL_DSN names.
func MySQLLookupDatabase(t *testing.T, setup ...string) LookupDatabase {
	t.Helper()
	serverDSN := envvar.Get(MySQLDSNEnv)
	if serverDSN == "" {
		t.Skipf("set %s to run this against a live MySQL instance", MySQLDSNEnv)
	}
	server, err := mysql.ParseDSN(serverDSN)
	if err != nil {
		t.Fatalf("read %s: %v", MySQLDSNEnv, err)
	}
	name, password := "lookup_"+randomHex(t, 4), "Rd-"+randomHex(t, 12)

	admin := openSQL(t, "mysql", serverDSN)
	mustExec(t, admin, "CREATE DATABASE "+name)
	mustExec(t, admin, fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY '%s'", name, password))
	t.Cleanup(func() {
		dropQuietly(admin, "DROP DATABASE IF EXISTS "+name)
		dropQuietly(admin, fmt.Sprintf("DROP USER IF EXISTS '%s'@'%%'", name))
	})

	adminDSN := mysqlDSN(server, server.User, server.Passwd, name)
	db := openSQL(t, "mysql", adminDSN)
	for _, statement := range setup {
		mustExec(t, db, statement)
	}
	mustExec(t, db, fmt.Sprintf("GRANT SELECT ON %s.* TO '%s'@'%%'", name, name))
	return LookupDatabase{DSN: mysqlDSN(server, name, password, name), AdminDSN: adminDSN}
}

// SQLServerLookupDatabase builds one on the server METIS_TEST_SQLSERVER_DSN
// names.
func SQLServerLookupDatabase(t *testing.T, setup ...string) LookupDatabase {
	t.Helper()
	serverDSN := envvar.Get(SQLServerDSNEnv)
	if serverDSN == "" {
		t.Skipf("set %s to run this against a live SQL Server instance", SQLServerDSNEnv)
	}
	// SQL Server enforces a password policy; this satisfies it.
	name, password := "lookup_"+randomHex(t, 4), "Rd-9x"+randomHex(t, 12)

	admin := openSQL(t, "sqlserver", serverDSN)
	mustExec(t, admin, "CREATE DATABASE "+name)
	mustExec(t, admin, fmt.Sprintf("CREATE LOGIN %s WITH PASSWORD = '%s', CHECK_POLICY = OFF", name, password))
	t.Cleanup(func() {
		dropQuietly(admin, "ALTER DATABASE "+name+" SET SINGLE_USER WITH ROLLBACK IMMEDIATE")
		dropQuietly(admin, "DROP DATABASE IF EXISTS "+name)
		dropQuietly(admin, "DROP LOGIN "+name)
	})

	adminDSN := sqlServerDSN(t, serverDSN, "", "", name)
	db := openSQL(t, "sqlserver", adminDSN)
	for _, statement := range setup {
		mustExec(t, db, statement)
	}
	mustExec(t, db, fmt.Sprintf("CREATE USER %s FOR LOGIN %s", name, name))
	mustExec(t, db, "ALTER ROLE db_datareader ADD MEMBER "+name)
	return LookupDatabase{DSN: sqlServerDSN(t, serverDSN, name, password, name), AdminDSN: adminDSN}
}

func postgresURL(server *pgx.ConnConfig, user, password, database string) string {
	sslMode := "disable"
	if server.TLSConfig != nil {
		sslMode = "require"
	}
	u := url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(user, password),
		Host:     server.Host + ":" + strconv.Itoa(int(server.Port)),
		Path:     "/" + database,
		RawQuery: "sslmode=" + sslMode,
	}
	return u.String()
}

func mysqlDSN(server *mysql.Config, user, password, database string) string {
	config := server.Clone()
	config.User, config.Passwd, config.DBName = user, password, database
	return config.FormatDSN()
}

// sqlServerDSN is the server's URL pointed at a database, and at another login
// when one is given.
func sqlServerDSN(t *testing.T, serverDSN, user, password, database string) string {
	t.Helper()
	u, err := url.Parse(serverDSN)
	if err != nil {
		t.Fatalf("read %s: %v", SQLServerDSNEnv, err)
	}
	if user != "" {
		u.User = url.UserPassword(user, password)
	}
	query := u.Query()
	query.Set("database", database)
	u.RawQuery = query.Encode()
	return u.String()
}

func openSQL(t *testing.T, driver, dsn string) *sql.DB {
	t.Helper()
	if driver == "pgx" {
		config, err := pgx.ParseConfig(dsn)
		if err != nil {
			t.Fatalf("read a PostgreSQL connection string: %v", err)
		}
		db := stdlib.OpenDB(*config)
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatalf("open %s: %v", driver, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), statement); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
}

// dropQuietly is cleanup: a database already gone is fine, and a failure here
// is a leftover on a test server, not a failed test.
func dropQuietly(db *sql.DB, statement string) {
	_, _ = db.ExecContext(context.Background(), statement)
}

func randomHex(t *testing.T, bytes int) string {
	t.Helper()
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random: %v", err)
	}
	return hex.EncodeToString(b)
}
