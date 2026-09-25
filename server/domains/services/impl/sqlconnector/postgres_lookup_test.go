package sqlconnector

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/tests/testutils"
)

// customersTable is the lookup target the PostgreSQL tests read: one row per
// type a decision is likely to be made on.
var customersTable = []string{
	`CREATE TABLE customers (
		id           int PRIMARY KEY,
		name         text NOT NULL,
		tier         text NOT NULL,
		credit_limit numeric(12, 2),
		since        date,
		active       boolean,
		preferences  jsonb,
		ref          uuid
	)`,
	`INSERT INTO customers VALUES
		(7, 'Acme', 'gold', 5000.00, '2019-03-01', true, '{"channel":"email"}', '0f8fad5b-d9cb-469f-a165-70867728950e'),
		(8, 'Globex', 'silver', 1200.50, '2021-07-15', false, NULL, NULL)`,
}

func testExecutor() *Executor {
	return &Executor{pools: newPools(poolSettings{maxConns: 2, maxPools: 4}, hostPolicy{})}
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func postgresConfig(dsn string, extra ...string) map[string]any {
	config := map[string]any{"driver": "postgres", "dsn": dsn}
	for i := 0; i+1 < len(extra); i += 2 {
		config[extra[i]] = extra[i+1]
	}
	return config
}

func TestAPostgresLookupReadsARowIntoOneVariable(t *testing.T) {
	target := testutils.PostgresLookupDatabase(t, customersTable...)
	answer, err := testExecutor().ExecuteRequest(testContext(t), postgresConfig(target.DSN), servicecontracts.ConnectorRequest{
		Statement:      "SELECT name, tier, credit_limit, since, active, preferences, ref FROM customers WHERE id = :customer_id",
		Params:         map[string]any{"customer_id": 7.0},
		ResultVariable: "customer",
	})
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(answer) != 1 {
		t.Fatalf("the lookup returned %d variables; it must return exactly its result variable: %v", len(answer), answer)
	}
	customer, _ := answer["customer"].(map[string]any)
	row, _ := customer[resultRow].(map[string]any)
	for column, want := range map[string]any{
		"name": "Acme", "tier": "gold", "credit_limit": 5000.0, "since": "2019-03-01", "active": true,
		"ref": "0f8fad5b-d9cb-469f-a165-70867728950e",
	} {
		if row[column] != want {
			t.Errorf("%s = %#v, want %#v", column, row[column], want)
		}
	}
	if prefs, _ := row["preferences"].(map[string]any); prefs["channel"] != "email" {
		t.Errorf("preferences were not read as JSON: %#v", row["preferences"])
	}
	if customer[resultRowCount] != 1.0 || customer[resultTruncated] != false {
		t.Errorf("row_count = %v, truncated = %v", customer[resultRowCount], customer[resultTruncated])
	}
}

func TestAPostgresLookupThatFindsNothingIsAnAnswer(t *testing.T) {
	target := testutils.PostgresLookupDatabase(t, customersTable...)
	answer, err := testExecutor().ExecuteRequest(testContext(t), postgresConfig(target.DSN), servicecontracts.ConnectorRequest{
		Statement:      "SELECT tier FROM customers WHERE id = :customer_id",
		Params:         map[string]any{"customer_id": 404.0},
		ResultVariable: "customer",
	})
	if err != nil {
		t.Fatalf("finding nothing was treated as a failure: %v", err)
	}
	customer, _ := answer["customer"].(map[string]any)
	if _, has := customer[resultRow]; has || customer[resultRowCount] != 0.0 {
		t.Fatalf("got %v", customer)
	}
}

// A value is a value. The name below is also valid SQL, and must find nobody.
func TestAPostgresLookupsValuesAreNeverSQL(t *testing.T) {
	target := testutils.PostgresLookupDatabase(t, customersTable...)
	answer, err := testExecutor().ExecuteRequest(testContext(t), postgresConfig(target.DSN), servicecontracts.ConnectorRequest{
		Statement:      "SELECT tier FROM customers WHERE name = :name",
		Params:         map[string]any{"name": "x' OR '1'='1"},
		ResultVariable: "customer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if customer, _ := answer["customer"].(map[string]any); customer[resultRowCount] != 0.0 {
		t.Fatalf("a value was read as SQL and matched %v rows", customer[resultRowCount])
	}
}

func TestAPostgresLookupStopsAtItsLimits(t *testing.T) {
	target := testutils.PostgresLookupDatabase(t,
		"CREATE TABLE events (n int, body text)",
		"INSERT INTO events SELECT g, repeat('x', 1000) FROM generate_series(1, 20) g")
	for name, tc := range map[string]struct {
		setting, value string
		wantRows       float64
	}{
		"rows":  {"max_rows", "5", 5},
		"bytes": {"max_result_bytes", "3500", 3},
	} {
		t.Run(name, func(t *testing.T) {
			answer, err := testExecutor().ExecuteRequest(testContext(t), postgresConfig(target.DSN, tc.setting, tc.value),
				servicecontracts.ConnectorRequest{Statement: "SELECT n, body FROM events ORDER BY n", ResultVariable: "events"})
			if err != nil {
				t.Fatal(err)
			}
			events, _ := answer["events"].(map[string]any)
			if events[resultRowCount] != tc.wantRows || events[resultTruncated] != true {
				t.Fatalf("row_count = %v, truncated = %v", events[resultRowCount], events[resultTruncated])
			}
		})
	}
}

func TestAPostgresLookupThatRunsTooLongIsStoppedByTheServer(t *testing.T) {
	target := testutils.PostgresLookupDatabase(t)
	started := time.Now()
	_, err := testExecutor().ExecuteRequest(testContext(t), postgresConfig(target.DSN, "statement_timeout_ms", "200"),
		servicecontracts.ConnectorRequest{
			Statement:      "SELECT count(*) FROM generate_series(1, 500000000) AS a",
			ResultVariable: "total",
		})
	if err == nil || !strings.Contains(err.Error(), "took longer than its limit") {
		t.Fatalf("a lookup past its limit was not stopped as one: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the lookup ran %s past a 200ms limit", elapsed)
	}
}

// The statement check is the second line on PostgreSQL. With it bypassed, the
// read-only transaction still stops a write — run as the administrator, who is
// allowed to write, so only the transaction can be what refuses it.
func TestPostgresRefusesAWriteEvenPastTheStatementCheck(t *testing.T) {
	target := testutils.PostgresLookupDatabase(t, customersTable...)
	conn, err := connectionFrom(postgresConfig(target.AdminDSN))
	if err != nil {
		t.Fatal(err)
	}
	ctx := testContext(t)
	db, err := testExecutor().pools.get(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	_, err = lookup(ctx, db, conn, "INSERT INTO customers (id, name, tier) VALUES (99, 'Intruder', 'gold') RETURNING id", nil)
	if err == nil || !isReadOnlyViolation(err) {
		t.Fatalf("the server did not refuse the write as read-only: %v", err)
	}
	assertCustomerCount(t, target.AdminDSN, 2)
}

// And the protocol holds a query to one statement, even when the connection
// string asks for the simple protocol that would run two.
func TestPostgresRunsOneStatementEvenWhenAskedForTheSimpleProtocol(t *testing.T) {
	target := testutils.PostgresLookupDatabase(t, customersTable...)
	conn, err := connectionFrom(postgresConfig(target.AdminDSN + "&default_query_exec_mode=simple_protocol"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := testContext(t)
	db, err := testExecutor().pools.get(ctx, conn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lookup(ctx, db, conn, "SELECT 1; DELETE FROM customers", nil); err == nil {
		t.Fatal("two statements ran as one query")
	}
	assertCustomerCount(t, target.AdminDSN, 2)
}

// Pointed at a database holding Metis's own tables, as a login that can see
// them, the connection is refused before any query runs.
func TestALookupThatCanSeeMetisOwnTablesIsRefused(t *testing.T) {
	testutils.SetupTestConn(t) // Metis's schema, in the database the server DSN names
	_, err := testExecutor().ExecuteRequest(testContext(t),
		postgresConfig(os.Getenv(testutils.PostgresDSNEnv)),
		servicecontracts.ConnectorRequest{Statement: "SELECT 1 AS one", ResultVariable: "x"})
	if !errors.Is(err, errOwnDatabase) {
		t.Fatalf("a lookup onto Metis's own database was allowed: %v", err)
	}
}

func assertCustomerCount(t *testing.T, adminDSN string, want int) {
	t.Helper()
	config, err := pgx.ParseConfig(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	db := stdlib.OpenDB(*config)
	defer func() { _ = db.Close() }()
	var count int
	if err := db.QueryRowContext(context.Background(), "SELECT count(*) FROM customers").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("customers has %d rows, want %d: something was written", count, want)
	}
}

func TestAPostgresLookupTakesAList(t *testing.T) {
	target := testutils.PostgresLookupDatabase(t, customersTable...)
	answer, err := testExecutor().ExecuteRequest(testContext(t), postgresConfig(target.DSN), servicecontracts.ConnectorRequest{
		Statement:      "SELECT name FROM customers WHERE id IN (:ids) ORDER BY id",
		Params:         map[string]any{"ids": []any{7.0, 8.0, 404.0}},
		ResultVariable: "customers",
	})
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	found, _ := answer["customers"].(map[string]any)
	rows, _ := found[resultRows].([]any)
	if len(rows) != 2 {
		t.Fatalf("got %v, want Acme and Globex", rows)
	}
	if first, _ := rows[0].(map[string]any); first["name"] != "Acme" {
		t.Fatalf("first row %v", rows[0])
	}
}

// The Connectors page's "Test" for a lookup reaches the database, and refuses
// Metis's own the way a lookup would.
func TestTestingAPostgresConnection(t *testing.T) {
	target := testutils.PostgresLookupDatabase(t)
	answer, err := testExecutor().Execute(testContext(t), postgresConfig(target.DSN), nil)
	if err != nil || answer["status"] != "connected" {
		t.Fatalf("got %v, %v", answer, err)
	}

	testutils.SetupTestConn(t)
	if _, err := testExecutor().Execute(testContext(t), postgresConfig(os.Getenv(testutils.PostgresDSNEnv)), nil); !errors.Is(err, errOwnDatabase) {
		t.Fatalf("testing a connection onto Metis's own tables was allowed: %v", err)
	}
}
