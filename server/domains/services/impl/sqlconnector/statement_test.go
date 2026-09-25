package sqlconnector

import (
	"errors"
	"strings"
	"testing"
)

var allDialects = map[string]dialect{
	"postgres":  postgresDialect{},
	"mysql":     mysqlDialect{},
	"sqlserver": sqlServerDialect{},
}

// Queries a lookup is for. Several are chosen because a naive check would
// refuse them: a keyword inside a string, a semicolon inside a string, a cast,
// a function that shares a name with a statement.
func TestAReadIsAllowed(t *testing.T) {
	for _, statement := range []string{
		"SELECT 1",
		"select id, name from customers where id = :customer_id",
		"WITH recent AS (SELECT * FROM orders WHERE placed_at > :since) SELECT count(*) FROM recent",
		"(SELECT 1) UNION (SELECT 2)",
		"SELECT 1;",
		"SELECT * FROM audit WHERE action IN ('create', 'update', 'delete')",
		"SELECT * FROM notes WHERE body = 'please update; then drop the old one'",
		"SELECT * FROM t WHERE name = 'it''s'",
		"SELECT * FROM t WHERE opens_at > '10:30'",
		`SELECT "Customer Name" FROM t`,
		"SELECT café, naïve FROM t",
		"SELECT a$b FROM t",
		"SELECT 1.5e3, 0xFF, .5",
		"SELECT REPLACE(name, 'a', 'b') FROM t",
		"SELECT * FROM securities WHERE isin = :isin",
		"SELECT @@VERSION",
	} {
		for name, d := range allDialects {
			if err := validateStatement(statement, d); err != nil {
				t.Errorf("%s refused a read it should allow: %q: %v", name, statement, err)
			}
		}
	}
}

func TestDialectSpecificReadsAreAllowed(t *testing.T) {
	for _, tc := range []struct {
		dialect   dialect
		statement string
	}{
		{postgresDialect{}, "SELECT created_at::date, payload->>'id' FROM events"},
		{mysqlDialect{}, "SELECT `order`, `group` FROM t"},
		{sqlServerDialect{}, "SELECT [order], [group] FROM dbo.t"},
		{sqlServerDialect{}, "SELECT TOP (10) * FROM t WITH (NOLOCK)"},
	} {
		if err := validateStatement(tc.statement, tc.dialect); err != nil {
			t.Errorf("%s refused %q: %v", tc.dialect.label(), tc.statement, err)
		}
	}
}

// Each of these either writes, runs something that is not the query, or is a
// way a validator reads a statement differently from the server that runs it.
func TestAnythingButAReadIsRefused(t *testing.T) {
	for reason, statement := range map[string]string{
		"empty":                              "   ",
		"a delete":                           "DELETE FROM customers",
		"an update":                          "UPDATE t SET a = 1",
		"not starting with SELECT or WITH":   "EXPLAIN ANALYZE SELECT 1",
		"TABLE shorthand":                    "TABLE customers",
		"two statements":                     "SELECT 1; SELECT 2",
		"a statement after a semicolon":      "SELECT 1; DELETE FROM t",
		"two separators":                     "SELECT 1;;",
		"T-SQL's second statement, no ;":     "SELECT * FROM t DROP TABLE t",
		"T-SQL EXEC after a SELECT":          "SELECT 1 EXEC xp_cmdshell 'dir'",
		"a keyword pressed against a number": "SELECT 1DELETE FROM t",
		"a data-changing CTE":                "WITH gone AS (DELETE FROM t RETURNING *) SELECT * FROM gone",
		"SELECT INTO":                        "SELECT * INTO copy_of_t FROM t",
		"a locking read":                     "SELECT * FROM t FOR UPDATE",
		"a lock hint":                        "SELECT * FROM t WITH (UPDLOCK)",
		"a line comment":                     "SELECT 1 -- note",
		"a block comment":                    "SELECT 1 /* note */",
		"MySQL's executable comment":         "SELECT 1 /*!50000 DROP TABLE t */",
		"a hash":                             "SELECT 1 # note",
		"dollar quoting":                     "SELECT $$x$$",
		"a positional ?":                     "SELECT * FROM t WHERE a = ?",
		"a function called by quoted name":   `SELECT "pg_read_file"('/etc/passwd')`,
		"a schema-qualified file read":       "SELECT pg_catalog.pg_read_file('/etc/passwd')",
		"a file read":                        "SELECT load_file('/etc/passwd')",
		"another server":                     "SELECT * FROM OPENROWSET(BULK 'c:\\x', SINGLE_CLOB) AS x",
		"a query run from a string":          "SELECT query_to_xml('DELETE FROM t RETURNING 1', true, true, '')",
		"sleeping on purpose":                "SELECT pg_sleep(100)",
		"an unterminated string":             "SELECT 'no end",
		"a no-break space before a keyword":  "SELECT 1\u00a0DELETE FROM t",
		"a control character":                "SELECT 1\x00",
		"a zero-width space":                 "SELECT 1\u200bDELETE",
		"session settings":                   "SELECT set_config('transaction_read_only', 'off', true)",
		"a transaction of its own":           "SELECT 1 COMMIT",
	} {
		for name, d := range allDialects {
			err := validateStatement(statement, d)
			var refusal *StatementError
			if !errors.As(err, &refusal) {
				t.Errorf("%s allowed %s: %q (err %v)", name, reason, statement, err)
			}
		}
	}
}

// Whether \' ends a string depends on server settings the connector cannot
// see. Read with backslash escapes, the DELETE below is inside a string;
// PostgreSQL with standard_conforming_strings on — its default — ends the
// string at \' and runs the DELETE as part of a CTE. Reading it both ways is
// what catches it.
func TestAStringThatEndsDifferentlyOnDifferentSettingsIsRefused(t *testing.T) {
	statement := `WITH a AS (SELECT 'x\'), gone AS (DELETE FROM t RETURNING 1) SELECT ('y') FROM a`
	for name, d := range allDialects {
		if err := validateStatement(statement, d); err == nil {
			t.Errorf("%s allowed a delete hidden behind a backslash", name)
		}
	}
	// And the other way round: MySQL escapes by default, so there the naive
	// reading is the one without escapes.
	statement = `SELECT 'a\' ; DELETE FROM t; SELECT '`
	for name, d := range allDialects {
		if err := validateStatement(statement, d); err == nil {
			t.Errorf("%s allowed a delete hidden behind an escaped quote", name)
		}
	}
}

func TestBracketsAreIdentifiersOnlyOnSQLServer(t *testing.T) {
	// On PostgreSQL [ ] is an array subscript, so what is between them is code
	// and must be read.
	statement := "SELECT tags[(SELECT pg_sleep(10))] FROM t"
	if err := validateStatement(statement, postgresDialect{}); err == nil {
		t.Error("PostgreSQL allowed a call hidden in an array subscript")
	}
}

func TestAnOverlongQueryIsRefused(t *testing.T) {
	statement := "SELECT " + strings.Repeat("1 + ", maxStatementBytes/4) + "1"
	if err := validateStatement(statement, postgresDialect{}); err == nil {
		t.Fatal("a query longer than the limit was allowed")
	}
}

func TestARefusalSaysWhatAndWhere(t *testing.T) {
	err := validateStatement("SELECT 1 FROM t DROP TABLE t", sqlServerDialect{})
	var refusal *StatementError
	if !errors.As(err, &refusal) {
		t.Fatalf("got %v", err)
	}
	if refusal.Position != 17 || !strings.Contains(refusal.Reason, "DROP") {
		t.Fatalf("the refusal does not point at the DROP: %+v", refusal)
	}
	if strings.Contains(err.Error(), "FROM t") {
		t.Fatalf("the refusal repeats the query: %v", err)
	}
}
