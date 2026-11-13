package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAListenAddressIsCheckedBeforeItIsWritten(t *testing.T) {
	for _, good := range []string{":8080", "0.0.0.0:8080", "10.0.0.5:9000", "[::]:8080", "metis.internal:80"} {
		if err := ValidateListenAddress(good); err != nil {
			t.Errorf("%q was refused: %v", good, err)
		}
	}
	for _, bad := range []string{"", "8080", ":0", ":70000", ":http", "bad host:8080", "a/b:80"} {
		if err := ValidateListenAddress(bad); err == nil {
			t.Errorf("%q was accepted; the server could not listen on it", bad)
		}
	}
}

// The server section round-trips through the file, and an empty one is not
// written at all: a release that does not know it must still read the file.
func TestTheServerSectionIsWrittenOnlyWhenSet(t *testing.T) {
	dir := t.TempDir()

	bare, err := NewConfig(DriverPostgres, "host=db", "an-encryption-key-of-enough-length", "a-jwt-secret")
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}
	barePath := filepath.Join(dir, "bare.yaml")
	if err := bare.Save(barePath); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if data := readFile(t, barePath); containsKey(data, "server:") {
		t.Fatalf("an empty server section was written:\n%s", data)
	}

	bare.Server = ServerConfig{HTTPAddress: "0.0.0.0:8080", PublicURL: "https://metis.example.com"}
	fullPath := filepath.Join(dir, "full.yaml")
	if err := bare.Save(fullPath); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(fullPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	values := loaded.Server.Values()
	if values[EnvHTTPAddress] != "0.0.0.0:8080" || values[EnvPublicURL] != "https://metis.example.com" {
		t.Fatalf("the server section did not round-trip: %+v", values)
	}
	if len((ServerConfig{}).Values()) != 0 {
		t.Fatal("an empty server section set something")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- a file this test wrote.
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func containsKey(data, key string) bool { return strings.Contains(data, key) }
