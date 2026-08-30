package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Which SQLite file a fresh start opens.
//
// The product's file is metis.db. An installation that predates the rename has
// a gobpm.db, and opening a fresh empty database beside it would present as
// total data loss — every process, task and definition gone, with no error to
// explain it.

func inTempDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
}

func TestAFreshInstallUsesTheNewName(t *testing.T) {
	inTempDir(t)
	if got := DefaultSQLitePath(); got != "metis.db" {
		t.Fatalf("DefaultSQLitePath = %q, want metis.db", got)
	}
}

func TestAnExistingDatabaseFromBeforeTheRenameIsOpened(t *testing.T) {
	inTempDir(t)
	if err := os.WriteFile("gobpm.db", []byte("not really sqlite, but present"), 0o600); err != nil {
		t.Fatalf("seed legacy file: %v", err)
	}
	if got := DefaultSQLitePath(); got != "gobpm.db" {
		t.Fatalf("DefaultSQLitePath = %q, want gobpm.db — ignoring it would read as total data loss", got)
	}
}

// Once both exist, the new one wins: an operator who has renamed their file and
// left a stale copy behind should get the one they moved to.
func TestTheNewNameWinsWhenBothExist(t *testing.T) {
	inTempDir(t)
	for _, name := range []string{"gobpm.db", "metis.db"} {
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	if got := DefaultSQLitePath(); got != "metis.db" {
		t.Fatalf("DefaultSQLitePath = %q, want metis.db", got)
	}
}

// A directory named gobpm.db is not a database. Stat succeeds on it, so a
// naive existence check would hand the driver something it cannot open.
func TestADirectoryIsNotMistakenForTheOldDatabase(t *testing.T) {
	inTempDir(t)
	if err := os.Mkdir("gobpm.db", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if got := DefaultSQLitePath(); got != "metis.db" {
		t.Fatalf("DefaultSQLitePath = %q, want metis.db — a directory is not a database file", got)
	}
}

// The configured path always wins; this resolver is only for when nothing was
// configured at all.
func TestAConfiguredPathIsUntouched(t *testing.T) {
	inTempDir(t)
	if err := os.WriteFile("gobpm.db", []byte("x"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	chosen := filepath.Join(t.TempDir(), "somewhere-else.db")
	if got := BuildConnectionString("sqlite", DatabaseFields{DBName: chosen}); got != chosen {
		t.Fatalf("BuildConnectionString = %q, want the configured path %q", got, chosen)
	}
}
