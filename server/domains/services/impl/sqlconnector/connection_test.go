package sqlconnector

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestConnectionSettings(t *testing.T) {
	conn, err := connectionFrom(map[string]any{
		"driver":               " Postgres ",
		"dsn":                  "postgres://svc@db.internal/crm",
		"statement_timeout_ms": "2500",
		"max_rows":             50.0,
		"max_result_bytes":     "999999999",
	})
	if err != nil {
		t.Fatal(err)
	}
	if conn.timeout != 2500*time.Millisecond {
		t.Errorf("timeout = %s", conn.timeout)
	}
	if conn.limits.rows != 50 {
		t.Errorf("max rows = %d", conn.limits.rows)
	}
	// Held to its ceiling rather than taken as written.
	if conn.limits.bytes != maxMaxResultBytes {
		t.Errorf("max bytes = %d, want the ceiling %d", conn.limits.bytes, maxMaxResultBytes)
	}
}

func TestMissingOrNonsenseLimitsTakeTheDefaults(t *testing.T) {
	conn, err := connectionFrom(map[string]any{
		"driver": "mysql", "dsn": "svc@tcp(db:3306)/crm",
		"statement_timeout_ms": "soon", "max_rows": -3.0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if conn.timeout != defaultTimeout || conn.limits.rows != defaultMaxRows || conn.limits.bytes != defaultMaxResultBytes {
		t.Fatalf("got %s / %d rows / %d bytes", conn.timeout, conn.limits.rows, conn.limits.bytes)
	}
}

func TestAConnectionMustSayWhatAndWhere(t *testing.T) {
	if _, err := connectionFrom(map[string]any{"dsn": "postgres://x"}); !errors.Is(err, errUnknownDriver) {
		t.Errorf("no driver: %v", err)
	}
	if _, err := connectionFrom(map[string]any{"driver": "sqlserver", "dsn": "  "}); !errors.Is(err, errNoDSN) {
		t.Errorf("no connection string: %v", err)
	}
}

// The pool key must not be the connection string: that holds the password,
// and a map key is the kind of thing that ends up in a heap dump or a log.
func TestThePoolKeyDoesNotHoldThePassword(t *testing.T) {
	conn := connection{driver: "postgres", dsn: "postgres://svc:hunter2@db/crm"}
	if key := conn.poolKey(); len(key) != 64 || strings.Contains(key, "hunter2") || strings.Contains(key, "svc") {
		t.Fatalf("pool key %q", key)
	}
	other := connection{driver: "mysql", dsn: conn.dsn}
	if other.poolKey() == conn.poolKey() {
		t.Fatal("the same string for two drivers shares a pool")
	}
}

// The catalogue offers exactly these three names; anything else is refused
// rather than guessed at.
func TestADriverNotInTheCatalogueIsRefused(t *testing.T) {
	for _, driver := range []string{"PostgreSQL", "mariadb", "sqlite", "oracle"} {
		if _, err := connectionFrom(map[string]any{"driver": driver, "dsn": "x"}); !errors.Is(err, errUnknownDriver) {
			t.Errorf("%q: %v", driver, err)
		}
	}
}
