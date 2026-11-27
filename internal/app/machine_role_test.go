package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gsoultan/metis/internal/pkg/config"
)

// A role the Servers page asked for is kept in config.yaml with nothing
// else in the file changed, and the file stays readable by its owner alone:
// it holds the database password and the keys.
func TestKeepRoleChangesOnlyTheRoleAndKeepsTheFilePrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	before := &config.Config{
		Database:      config.DatabaseConfig{Driver: config.DriverPostgres, EncryptedConnection: "c2VjcmV0"},
		EncryptionKey: "the-key",
		JWTSecret:     "the-secret",
		Server:        config.ServerConfig{HTTPAddress: ":8080", Role: "all", Purpose: "production", Name: "Jakarta"},
	}
	if err := before.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := keepRole(path, "worker"); err != nil {
		t.Fatalf("keep the role: %v", err)
	}

	after, err := config.Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := *before
	want.Server.Role = "worker"
	if after.Database != want.Database || after.EncryptionKey != want.EncryptionKey ||
		after.JWTSecret != want.JWTSecret || after.Server != want.Server {
		t.Fatalf("config.yaml after the change = %+v, want %+v", after, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("config.yaml mode = %o, want 600", mode)
	}
	left, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".config-*"))
	if err != nil || len(left) != 0 {
		t.Fatalf("a temporary file was left behind: %v %v", left, err)
	}
}

func TestKeepRoleWithoutAConfigFileFails(t *testing.T) {
	if err := keepRole(filepath.Join(t.TempDir(), "missing.yaml"), "web"); err == nil {
		t.Fatal("a role was kept with no config.yaml to keep it in")
	}
}

// METIS_ROLE wins over config.yaml, so a role kept there would change
// nothing: the operator's choice is fixed.
func TestARoleSetInTheEnvironmentIsFixed(t *testing.T) {
	t.Setenv(envRole, "worker")
	if !roleFixed() {
		t.Fatal("a role set with METIS_ROLE could be changed from the Servers page")
	}
}
