package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpgradeCommandNeedsARelease(t *testing.T) {
	var out bytes.Buffer
	err := (&App{}).runUpgradeCommand(t.Context(), nil, &out)
	if err == nil || !strings.Contains(err.Error(), "--to") {
		t.Fatalf("err %v", err)
	}
}

// A test binary is never /usr/bin/metis, so whoever runs it, the command
// refuses before touching anything.
func TestUpgradeCommandRefusesOutsideAPackageInstall(t *testing.T) {
	var out bytes.Buffer
	err := (&App{}).runUpgradeCommand(t.Context(), []string{"--to", "v9.9.9"}, &out)
	if err == nil {
		t.Fatal("the upgrade ran from a test binary")
	}
	if !strings.Contains(err.Error(), "runs as root") && !strings.Contains(err.Error(), "not installed from a Linux package") {
		t.Fatalf("err %v", err)
	}
}

func TestTheServiceEnvironmentWinsOverTheShell(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://stray")
	path := filepath.Join(t.TempDir(), "metis.env")
	if err := os.WriteFile(path, []byte("DATABASE_URL=postgres://service\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := loadServiceEnvironment(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("DATABASE_URL"); got != "postgres://service" {
		t.Fatalf("DATABASE_URL = %q", got)
	}
	if err := loadServiceEnvironment(filepath.Join(t.TempDir(), "missing")); err != nil {
		t.Fatalf("a missing file: %v", err)
	}
}
