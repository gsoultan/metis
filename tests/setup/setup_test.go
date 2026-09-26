package setup_test

import (
	"os"
	"strings"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/config"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	"github.com/gsoultan/metis/server/domains/services/impl"
)

// An engine this no longer runs on is refused by name, at the wizard, rather
// than at the first query.
func TestTestConnection_RetiredDriverIsRefused(t *testing.T) {
	svc := impl.NewSetupService(nil)

	for _, driver := range []string{"sqlite", "mysql", "sqlserver"} {
		result := svc.TestConnection(t.Context(), contracts.TestConnectionRequest{
			DatabaseDriver: driver,
			DBName:         "metis",
		})
		if result.Success {
			t.Fatalf("%s reported a successful connection; it is not an engine this supports", driver)
		}
		if !strings.Contains(result.Message, "PostgreSQL") {
			t.Errorf("the refusal for %s does not say what to use instead: %q", driver, result.Message)
		}
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

func TestTheConnectionTestClosesOnceConfigured(t *testing.T) {
	dir := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	svc := impl.NewSetupService(nil)
	probe := contracts.TestConnectionRequest{
		DatabaseDriver: "postgres",
		DBHost:         "127.0.0.1",
		DBPort:         1,
		DBUsername:     "probe",
		DBPassword:     "probe",
		DBName:         "probe",
	}

	// Unconfigured: the wizard needs this, and it answers with what it found.
	before := svc.TestConnection(t.Context(), probe)
	if !strings.Contains(before.Message, "connect") && !strings.Contains(before.Message, "refused") {
		t.Fatalf("while unconfigured the wizard should report what happened, got %q", before.Message)
	}

	// Configured is what config.yaml existing means — the same thing Setup checks.
	if err := os.WriteFile(config.DefaultConfigPath, []byte("database:\n  driver: sqlite\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	after := svc.TestConnection(t.Context(), probe)
	if after.Success {
		t.Fatal("a configured installation must not run connection probes for anonymous callers")
	}
	if strings.Contains(after.Message, "127.0.0.1") || strings.Contains(after.Message, "refused") {
		t.Fatalf("the reply must not say what it found at the address, got %q", after.Message)
	}
	if !strings.Contains(after.Message, "already configured") {
		t.Fatalf("the refusal should say why and where to go instead, got %q", after.Message)
	}
}
