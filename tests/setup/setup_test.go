package setup_test

import (
	"testing"

	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

func TestTestConnection_SQLiteSuccess(t *testing.T) {
	svc := impl.NewSetupService(nil)

	result := svc.TestConnection(t.Context(), contracts.TestConnectionRequest{
		DatabaseDriver: "sqlite",
		DBName:         ":memory:",
	})

	if !result.Success {
		t.Fatalf("expected success, got failure: %s", result.Message)
	}
	if result.Message != "Connection successful" {
		t.Errorf("expected 'Connection successful', got %q", result.Message)
	}
}

func TestTestConnection_EmptyDriver(t *testing.T) {
	svc := impl.NewSetupService(nil)

	result := svc.TestConnection(t.Context(), contracts.TestConnectionRequest{
		DatabaseDriver: "",
	})

	if result.Success {
		t.Fatal("expected failure for empty driver")
	}
	if result.Message != "Database driver is required" {
		t.Errorf("expected 'Database driver is required', got %q", result.Message)
	}
}

func TestTestConnection_InvalidHost(t *testing.T) {
	svc := impl.NewSetupService(nil)

	result := svc.TestConnection(t.Context(), contracts.TestConnectionRequest{
		DatabaseDriver: "postgres",
		DBHost:         "invalid-host-that-does-not-exist.local",
		DBPort:         5432,
		DBUsername:     "test",
		DBPassword:     "test",
		DBName:         "test",
	})

	if result.Success {
		t.Fatal("expected failure for invalid host")
	}
	if result.Message == "" {
		t.Error("expected non-empty error message")
	}
}

func TestTestConnection_SQLiteDefaultPath(t *testing.T) {
	svc := impl.NewSetupService(nil)

	// SQLite with empty DBName should use the default file (metis.db, or an
	// existing gobpm.db from before the rename).
	result := svc.TestConnection(t.Context(), contracts.TestConnectionRequest{
		DatabaseDriver: "sqlite",
	})

	if !result.Success {
		t.Fatalf("expected success for SQLite default path, got failure: %s", result.Message)
	}
}
