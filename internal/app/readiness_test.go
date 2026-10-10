package app

import (
	"context"
	"os"
	"testing"
	"time"

	stormdb "github.com/gsoultan/metis/server/repositories/db"
)

// Readiness has to ask the pool that serves the traffic.
//
// Only the GORM pool was pinged, while the pgx pool carries most of the
// engine's queries; a replica whose pgx pool could not reach the database
// reported ready and stayed in rotation, failing every request it was sent.
func TestReadinessFailsWhenThePgxPoolCannotReachTheDatabase(t *testing.T) {
	// Nothing listens on port 1. The pool connects lazily, so it opens and
	// fails only when asked.
	conn, err := stormdb.Open(t.Context(), "postgres://metis:metis@127.0.0.1:1/metis?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(conn.Close)

	a := &App{storm: conn}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var failed []string
	for name, checker := range a.readinessCheckers() {
		if checker.Check(ctx) != nil {
			failed = append(failed, name)
		}
	}
	if len(failed) == 0 {
		t.Fatal("every readiness check passed while the pgx pool could not reach its database")
	}
}

// A pgx pool that reaches its database is ready.
func TestReadinessPassesWhenThePgxPoolReachesTheDatabase(t *testing.T) {
	dsn := os.Getenv("STORM_DSN")
	if dsn == "" {
		t.Skip("STORM_DSN is not set")
	}
	conn, err := stormdb.Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(conn.Close)

	a := &App{storm: conn}
	for name, checker := range a.readinessCheckers() {
		if err := checker.Check(t.Context()); err != nil {
			t.Fatalf("%s failed against a reachable database: %v", name, err)
		}
	}
}

// Before setup there is no storm connection, and that is not a failure.
func TestReadinessPassesWithNoPgxPoolYet(t *testing.T) {
	a := &App{}
	for name, checker := range a.readinessCheckers() {
		if err := checker.Check(t.Context()); err != nil {
			t.Fatalf("%s failed with nothing configured: %v", name, err)
		}
	}
}
