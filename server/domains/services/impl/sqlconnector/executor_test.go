package sqlconnector

import (
	"context"
	"errors"
	"testing"

	servicecontracts "github.com/gsoultan/metis/server/domains/services/contracts"
)

// A connector call with no step behind it arrives from POST
// /connectors/execute, with a configuration its caller wrote. The lookup must
// refuse it without connecting anywhere, or that endpoint becomes a way to try
// any database host and credential the server can reach.
func TestALookupWithNoStepConnectsNowhere(t *testing.T) {
	e := &Executor{pools: newPools(poolSettings{maxConns: 1, maxPools: 1}, hostPolicy{})}
	_, err := e.Execute(context.Background(), map[string]any{
		"driver": "postgres", "dsn": "postgres://svc:pw@10.0.0.1:5432/anything",
	}, map[string]any{})
	if !errors.Is(err, errNoRequest) {
		t.Fatalf("got %v", err)
	}
	if e.pools.cache.Len() != 0 {
		t.Fatal("a pool was opened for a call with no step")
	}
}

// Everything that can be refused before connecting is.
func TestARequestIsCheckedBeforeAnyConnection(t *testing.T) {
	e := &Executor{pools: newPools(poolSettings{maxConns: 1, maxPools: 1}, hostPolicy{})}
	config := map[string]any{"driver": "postgres", "dsn": "postgres://svc:pw@203.0.113.1:5432/crm"}

	for name, req := range map[string]servicecontracts.ConnectorRequest{
		"no result variable":            {Statement: "SELECT 1"},
		"a result variable with spaces": {Statement: "SELECT 1", ResultVariable: "the customer"},
		"engine bookkeeping's prefix":   {Statement: "SELECT 1", ResultVariable: "_join_x"},
		"a write":                       {Statement: "DELETE FROM t", ResultVariable: "x"},
		"a missing parameter":           {Statement: "SELECT * FROM t WHERE id = :id", ResultVariable: "x"},
	} {
		if _, err := e.ExecuteRequest(context.Background(), config, req); err == nil {
			t.Errorf("%s was not refused", name)
		}
	}
	if e.pools.cache.Len() != 0 {
		t.Fatal("a request refused on its face still opened a pool")
	}
}

func TestTheAnswerIsOneVariable(t *testing.T) {
	found := rowsRead{rows: []map[string]any{{"tier": "gold"}, {"tier": "silver"}}}.envelope()
	if found[resultRowCount] != 2.0 || found[resultTruncated] != false {
		t.Fatalf("got %v", found)
	}
	if row, _ := found[resultRow].(map[string]any); row["tier"] != "gold" {
		t.Fatalf("row is not the first row: %v", found[resultRow])
	}

	// Nothing found is an answer, not an error, and has no row to read.
	empty := rowsRead{}.envelope()
	if _, has := empty[resultRow]; has || empty[resultRowCount] != 0.0 {
		t.Fatalf("got %v", empty)
	}
}

func TestHostPolicy(t *testing.T) {
	if err := hostPolicyOf("").permit("anywhere.example.com"); err != nil {
		t.Errorf("an empty policy refused a host: %v", err)
	}
	policy := hostPolicyOf(" DB.internal , replica.internal,")
	if err := policy.permit("db.internal", "Replica.Internal"); err != nil {
		t.Errorf("a listed host was refused: %v", err)
	}
	if err := policy.permit("db.internal", "elsewhere.internal"); !errors.Is(err, errHostNotAllowed) {
		t.Errorf("a connection that can reach an unlisted fallback host was allowed: %v", err)
	}
	if hostOf("db.internal:3306") != "db.internal" || hostOf("/var/run/mysqld.sock") != "/var/run/mysqld.sock" {
		t.Error("hostOf")
	}
}

// The host policy is applied while the connection string is read, before
// anything is dialled.
func TestEachDialectHoldsItsHostsToThePolicy(t *testing.T) {
	policy := hostPolicyOf("allowed.internal")
	for name, tc := range map[string]struct {
		d   dialect
		dsn string
	}{
		"postgres":          {postgresDialect{}, "postgres://svc:pw@other.internal:5432/crm"},
		"postgres fallback": {postgresDialect{}, "postgres://svc:pw@allowed.internal,other.internal/crm"},
		"mysql":             {mysqlDialect{}, "svc:pw@tcp(other.internal:3306)/crm"},
		"sqlserver":         {sqlServerDialect{}, "sqlserver://svc:pw@other.internal:1433?database=crm"},
	} {
		if _, err := tc.d.open(tc.dsn, policy); !errors.Is(err, errHostNotAllowed) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// A driver's parse error can quote the connection string, password and all.
func TestAnUnreadableConnectionStringIsNotRepeated(t *testing.T) {
	for name, tc := range map[string]struct {
		d   dialect
		dsn string
	}{
		"postgres":  {postgresDialect{}, "postgres://svc:hunter2@[bad"},
		"mysql":     {mysqlDialect{}, "svc:hunter2@tcp(db:3306"},
		"sqlserver": {sqlServerDialect{}, "sqlserver://svc:hunter2@db:notaport"},
	} {
		_, err := tc.d.open(tc.dsn, hostPolicy{})
		if !errors.Is(err, errUnreadableConnection) {
			t.Errorf("%s: %v", name, err)
		}
	}
}
