package participantsource_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gsoultan/metis/internal/pkg/envvar"
	"github.com/gsoultan/metis/server/domains/services/impl/participantsource"
	"github.com/gsoultan/metis/tests/testutils"
)

// seedDirectory builds somebody else's directory: its own database, read by
// a login that cannot see Metis's tables, which is the only kind of database a
// source is allowed to read.
func seedDirectory(t *testing.T) (dsn, namespace string) {
	t.Helper()
	namespace = "dir_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	target := testutils.PostgresLookupDatabase(t, fmt.Sprintf(`
		CREATE SCHEMA %s;
		CREATE TABLE %s.staff (
			login       text primary key,
			full_name   text,
			work_email  text,
			team        text,
			is_current  boolean
		);
		INSERT INTO %s.staff VALUES
			('ada',   'Ada Lovelace', 'ada@example.com',   'approvers', true),
			('bob',   'Bob Vance',    'bob@example.com',   NULL,        false),
			('carol', NULL,           'not-an-address',    'finance',   true);
	`, namespace, namespace, namespace),
		fmt.Sprintf("GRANT USAGE ON SCHEMA %s TO PUBLIC", namespace),
		// DELETE as well: a login that could write is the case that matters,
		// since an administrator's reader is not always as narrow as it should be.
		fmt.Sprintf("GRANT SELECT, DELETE ON ALL TABLES IN SCHEMA %s TO PUBLIC", namespace),
	)
	return target.DSN, namespace
}

// A query against somebody else's table, aliased into the shared vocabulary.
//
// Aliasing rather than a per-source field mapping is deliberate: the names are
// the same across CSV, JSON and SQL, so somebody who has imported a file
// already knows what a query has to return.
func TestASQLDirectoryIsFetchedAndValidated(t *testing.T) {
	dsn, namespace := seedDirectory(t)

	source := participantsource.NewPostgresSource(participantsource.PostgresConfig{
		DSN: dsn,
		Query: fmt.Sprintf(`
			SELECT login AS username,
			       full_name AS display_name,
			       work_email AS email,
			       team AS groups,
			       is_current AS active
			FROM %s.staff ORDER BY login`, namespace),
	})

	result, err := source.Fetch(t.Context())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}

	if len(result.Rows) != 2 {
		t.Fatalf("ada and bob are usable, carol's address is not: got %d rows (%v)", len(result.Rows), result.Problems)
	}
	if result.Rows[0].Username != "ada" || result.Rows[0].DisplayName != "Ada Lovelace" {
		t.Errorf("ada read wrongly: %+v", result.Rows[0])
	}
	// A SQL boolean means the same thing as the CSV's yes/no and JSON's true.
	if !result.Rows[0].Active || result.Rows[1].Active {
		t.Errorf("active read wrongly: %v / %v", result.Rows[0].Active, result.Rows[1].Active)
	}
	// A NULL column is absent, not the string "null".
	if result.Rows[1].DisplayName == "" && len(result.Rows[1].Groups) != 0 {
		t.Errorf("a NULL group should be no groups, got %v", result.Rows[1].Groups)
	}
	// And the same validation as every other source.
	if len(result.Problems) != 1 || result.Problems[0].Username != "carol" {
		t.Fatalf("carol's address should be refused, got %v", result.Problems)
	}
}

// Columns the vocabulary does not know are ignored, the same as an unknown CSV
// column: a real directory table has columns that mean nothing here.
func TestUnknownColumnsAreIgnored(t *testing.T) {
	dsn, namespace := seedDirectory(t)

	source := participantsource.NewPostgresSource(participantsource.PostgresConfig{
		DSN:   dsn,
		Query: fmt.Sprintf("SELECT login AS username, team AS cost_centre FROM %s.staff WHERE login = 'ada'", namespace),
	})
	result, err := source.Fetch(t.Context())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(result.Rows) != 1 || result.Rows[0].Username != "ada" {
		t.Fatalf("expected ada, got %+v (%v)", result.Rows, result.Problems)
	}
}

// A query that returns no username produces problems, not participants: the
// vocabulary is the contract, and a query that ignores it has not been written
// yet.
func TestAQueryWithoutAUsernameProducesProblems(t *testing.T) {
	dsn, namespace := seedDirectory(t)

	source := participantsource.NewPostgresSource(participantsource.PostgresConfig{
		DSN:   dsn,
		Query: fmt.Sprintf("SELECT full_name, work_email FROM %s.staff", namespace),
	})
	result, err := source.Fetch(t.Context())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(result.Rows) != 0 {
		t.Fatalf("no row names a participant, got %d", len(result.Rows))
	}
	if len(result.Problems) != 3 {
		t.Fatalf("every row should be reported, got %v", result.Problems)
	}
}

// A source that cannot be read fails as a source.
func TestAnUnreadableSourceFails(t *testing.T) {
	dsn, namespace := seedDirectory(t)

	for _, tc := range []struct{ name, dsn, query string }{
		{"no query", dsn, "   "},
		{"no database", "", "SELECT 1 AS username"},
		{"broken query", dsn, fmt.Sprintf("SELECT nope AS username FROM %s.staff", namespace)},
		{"unreachable database", "postgres://nobody:x@127.0.0.1:1/none?sslmode=disable", "SELECT 1 AS username"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := participantsource.NewPostgresSource(participantsource.PostgresConfig{DSN: tc.dsn, Query: tc.query})
			if _, err := source.Fetch(t.Context()); err == nil {
				t.Fatalf("%s should fail", tc.name)
			}
		})
	}
}

// Metis's own database is never a directory. A source pointed at it would run
// a project administrator's SQL against every organization's tables.
func TestMetisOwnDatabaseIsRefused(t *testing.T) {
	// Migrates Metis's tables into the test server, as an installation has.
	testutils.SetupTestConn(t)
	dsn := envvar.Get(testutils.PostgresDSNEnv)
	source := participantsource.NewPostgresSource(participantsource.PostgresConfig{
		DSN:   dsn,
		Query: "SELECT username FROM workflow_users",
	})
	_, err := source.Fetch(t.Context())
	if err == nil || !strings.Contains(err.Error(), "Metis's own tables") {
		t.Fatalf("a login that can see Metis's tables must be refused, got %v", err)
	}
}

// A directory query only reads. Whatever the SQL says, the transaction it runs
// in cannot write, so a query that tries is refused by the database itself.
func TestADirectoryQueryCannotWrite(t *testing.T) {
	dsn, namespace := seedDirectory(t)
	for _, query := range []string{
		fmt.Sprintf("DELETE FROM %s.staff RETURNING login AS username", namespace),
		fmt.Sprintf("WITH gone AS (DELETE FROM %s.staff RETURNING login) SELECT login AS username FROM gone", namespace),
	} {
		source := participantsource.NewPostgresSource(participantsource.PostgresConfig{DSN: dsn, Query: query})
		if _, err := source.Fetch(t.Context()); err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Fatalf("%q should be refused as a write, got %v", query, err)
		}
	}
	// And nothing was deleted.
	source := participantsource.NewPostgresSource(participantsource.PostgresConfig{
		DSN: dsn, Query: fmt.Sprintf("SELECT login AS username FROM %s.staff", namespace),
	})
	result, err := source.Fetch(t.Context())
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(result.Rows)+len(result.Problems) != 3 {
		t.Fatalf("the directory should still have three people, got %d", len(result.Rows)+len(result.Problems))
	}
}

// One query is one statement: a second statement after a semicolon is not run.
func TestADirectoryQueryIsOneStatement(t *testing.T) {
	dsn, namespace := seedDirectory(t)
	source := participantsource.NewPostgresSource(participantsource.PostgresConfig{
		DSN:   dsn + "&default_query_exec_mode=simple_protocol",
		Query: fmt.Sprintf("SELECT login AS username FROM %s.staff; SELECT 1", namespace),
	})
	if _, err := source.Fetch(t.Context()); err == nil {
		t.Fatal("two statements in one query should be refused")
	}
}

// When the operator lists the database hosts a connection may reach, a source
// naming any other host is refused before anything is dialled.
func TestASourceOutsideTheAllowedHostsIsRefused(t *testing.T) {
	t.Setenv("METIS_SQL_LOOKUP_ALLOWED_HOSTS", "db.allowed.example")
	source := participantsource.NewPostgresSource(participantsource.PostgresConfig{
		DSN:   "postgres://reader:x@169.254.169.254:5432/hr?sslmode=disable",
		Query: "SELECT 1 AS username",
	})
	_, err := source.Fetch(t.Context())
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("a host outside the list should be refused, got %v", err)
	}
}
