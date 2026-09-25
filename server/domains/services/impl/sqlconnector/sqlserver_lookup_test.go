package sqlconnector

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/tests/testutils"
)

var sqlServerCustomers = []string{
	`CREATE TABLE dbo.customers (
		id           INT PRIMARY KEY,
		name         NVARCHAR(100) NOT NULL,
		tier         NVARCHAR(20) NOT NULL,
		credit_limit DECIMAL(12, 2),
		since        DATE,
		active       BIT,
		ref          UNIQUEIDENTIFIER
	)`,
	`INSERT INTO dbo.customers VALUES
		(7, N'Acme', N'gold', 5000.00, '2019-03-01', 1, '0F8FAD5B-D9CB-469F-A165-70867728950E'),
		(8, N'Globex', N'silver', 1200.50, '2021-07-15', 0, NULL)`,
}

func sqlServerConfig(dsn string, extra ...string) map[string]any {
	config := map[string]any{"driver": "sqlserver", "dsn": dsn}
	for i := 0; i+1 < len(extra); i += 2 {
		config[extra[i]] = extra[i+1]
	}
	return config
}

func TestASQLServerLookupReadsARowIntoOneVariable(t *testing.T) {
	target := testutils.SQLServerLookupDatabase(t, sqlServerCustomers...)
	answer, err := testExecutor().ExecuteRequest(testContext(t), sqlServerConfig(target.DSN), servicecontracts.ConnectorRequest{
		Statement:      "SELECT [name], tier, credit_limit, since, active, ref FROM dbo.customers WHERE id = :customer_id",
		Params:         map[string]any{"customer_id": 7.0},
		ResultVariable: "customer",
	})
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	customer, _ := answer["customer"].(map[string]any)
	row, _ := customer[resultRow].(map[string]any)
	for column, want := range map[string]any{
		"name": "Acme", "tier": "gold", "credit_limit": 5000.0, "since": "2019-03-01", "active": true,
		// The id as everybody else writes it, not SQL Server's byte-swapped storage.
		"ref": "0F8FAD5B-D9CB-469F-A165-70867728950E",
	} {
		if row[column] != want {
			t.Errorf("%s = %#v, want %#v", column, row[column], want)
		}
	}
}

// SQL Server cannot begin a read-only transaction. What it has is the
// transaction every lookup runs in being rolled back: run as the
// administrator, a write that got past the statement check is undone.
func TestSQLServerUndoesAWriteThatGotPastTheStatementCheck(t *testing.T) {
	target := testutils.SQLServerLookupDatabase(t, sqlServerCustomers...)
	conn, db := openForTest(t, sqlServerConfig(target.AdminDSN))
	// Counting inside the same transaction proves the insert really ran; the
	// count afterwards proves it was really undone. Without the first, a batch
	// that failed for some other reason would pass this test.
	query := "INSERT INTO dbo.customers (id, name, tier) VALUES (99, N'Intruder', N'gold'); " +
		"SELECT COUNT(*) AS n FROM dbo.customers"
	read, err := lookup(testContext(t), db, conn, query, nil)
	if err != nil {
		t.Fatalf("the batch did not run, so this proves nothing: %v", err)
	}
	if len(read.rows) != 1 || read.rows[0]["n"] != 3.0 {
		t.Fatalf("inside the lookup the table held %v rows; the insert did not run", read.rows)
	}
	assertSQLServerCustomerCount(t, target.AdminDSN, 2)
}

func TestASQLServerLookupThatRunsTooLongIsStopped(t *testing.T) {
	target := testutils.SQLServerLookupDatabase(t)
	_, err := testExecutor().ExecuteRequest(testContext(t), sqlServerConfig(target.DSN, "statement_timeout_ms", "300"),
		servicecontracts.ConnectorRequest{
			Statement:      "SELECT COUNT_BIG(*) AS total FROM sys.all_objects a CROSS JOIN sys.all_objects b CROSS JOIN sys.all_objects c",
			ResultVariable: "total",
		})
	if err == nil || !strings.Contains(err.Error(), "took longer than its limit") {
		t.Fatalf("a lookup past its limit was not stopped as one: %v", err)
	}
}

func assertSQLServerCustomerCount(t *testing.T, adminDSN string, want int) {
	t.Helper()
	db, err := sql.Open("sqlserver", adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var count int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM dbo.customers").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("customers has %d rows, want %d: a write survived the rollback", count, want)
	}
}
