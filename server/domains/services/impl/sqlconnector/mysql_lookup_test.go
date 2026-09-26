package sqlconnector

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"

	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/tests/testutils"
)

var mysqlCustomers = []string{
	`CREATE TABLE customers (
		id           INT PRIMARY KEY,
		name         VARCHAR(100) NOT NULL,
		tier         VARCHAR(20) NOT NULL,
		credit_limit DECIMAL(12, 2),
		since        DATE,
		active       TINYINT(1),
		preferences  JSON
	)`,
	`INSERT INTO customers VALUES
		(7, 'Acme', 'gold', 5000.00, '2019-03-01', 1, '{"channel":"email"}'),
		(8, 'Globex', 'silver', 1200.50, '2021-07-15', 0, NULL)`,
}

func mysqlConfig(dsn string, extra ...string) map[string]any {
	config := map[string]any{"driver": "mysql", "dsn": dsn}
	for i := 0; i+1 < len(extra); i += 2 {
		config[extra[i]] = extra[i+1]
	}
	return config
}

func TestAMySQLLookupReadsARowIntoOneVariable(t *testing.T) {
	target := testutils.MySQLLookupDatabase(t, mysqlCustomers...)
	answer, err := testExecutor().ExecuteRequest(testContext(t), mysqlConfig(target.DSN), servicecontracts.ConnectorRequest{
		Statement:      "SELECT name, tier, credit_limit, since, active, preferences FROM customers WHERE id = :customer_id",
		Params:         map[string]any{"customer_id": 7.0},
		ResultVariable: "customer",
	})
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	customer, _ := answer["customer"].(map[string]any)
	row, _ := customer[resultRow].(map[string]any)
	// TINYINT(1) is how MySQL spells a boolean, and it arrives as a number.
	for column, want := range map[string]any{
		"name": "Acme", "tier": "gold", "credit_limit": 5000.0, "since": "2019-03-01", "active": 1.0,
	} {
		if row[column] != want {
			t.Errorf("%s = %#v, want %#v", column, row[column], want)
		}
	}
	if prefs, _ := row["preferences"].(map[string]any); prefs["channel"] != "email" {
		t.Errorf("preferences were not read as JSON: %#v", row["preferences"])
	}
}

func TestMySQLRefusesAWriteEvenPastTheStatementCheck(t *testing.T) {
	target := testutils.MySQLLookupDatabase(t, mysqlCustomers...)
	conn, db := openForTest(t, mysqlConfig(target.AdminDSN))
	_, err := lookup(testContext(t), db, conn, "INSERT INTO customers (id, name, tier) VALUES (99, 'Intruder', 'gold')", nil)
	if err == nil || !isReadOnlyViolation(err) {
		t.Fatalf("the server did not refuse the write as read-only: %v", err)
	}
	assertMySQLCustomerCount(t, target.AdminDSN, 2)
}

func TestMySQLRunsOneStatementEvenWhenAskedForMore(t *testing.T) {
	target := testutils.MySQLLookupDatabase(t, mysqlCustomers...)
	asked, err := mysql.ParseDSN(target.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	asked.MultiStatements = true
	conn, db := openForTest(t, mysqlConfig(asked.FormatDSN()))
	if _, err := lookup(testContext(t), db, conn, "SELECT 1; DELETE FROM customers", nil); err == nil {
		t.Fatal("two statements ran as one query")
	}
	assertMySQLCustomerCount(t, target.AdminDSN, 2)
}

func TestAMySQLLookupThatRunsTooLongIsStopped(t *testing.T) {
	target := testutils.MySQLLookupDatabase(t,
		"CREATE TABLE numbers (n INT)",
		"INSERT INTO numbers (n) WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < 1000) SELECT n FROM seq")
	_, err := testExecutor().ExecuteRequest(testContext(t), mysqlConfig(target.DSN, "statement_timeout_ms", "200"),
		servicecontracts.ConnectorRequest{
			// A checksum of all three together, so every one of the 10^9
			// combinations has to be visited. A bare COUNT(*) of the cross join came
			// back in 40ms on MySQL 8.4; a remainder of a sum or product could in
			// principle be pre-grouped per input, as SQL Server showed.
			Statement:      "SELECT COUNT(*) AS total FROM numbers a, numbers b, numbers c WHERE CRC32(CONCAT(a.n, ':', b.n, ':', c.n)) % 7 = 3",
			ResultVariable: "total",
		})
	if err == nil || !strings.Contains(err.Error(), "took longer than its limit") {
		t.Fatalf("a lookup past its limit was not stopped as one: %v", err)
	}
}

func openForTest(t *testing.T, config map[string]any) (connection, *sql.DB) {
	t.Helper()
	conn, err := connectionFrom(config)
	if err != nil {
		t.Fatal(err)
	}
	db, err := testExecutor().pools.get(testContext(t), conn)
	if err != nil {
		t.Fatal(err)
	}
	return conn, db
}

func assertMySQLCustomerCount(t *testing.T, adminDSN string, want int) {
	t.Helper()
	db, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var count int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM customers").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("customers has %d rows, want %d: something was written", count, want)
	}
}
